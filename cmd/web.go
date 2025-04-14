// Copyright © 2020 Ulrich Anhalt <ulrich.anhalt@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type collector struct {
	// possible metric descriptions.
	Desc *prometheus.Desc

	// a parameterized function used to gather metrics.
	stats func() []MetricData
}

// MetricData - metric data
type MetricData struct {
	Name       string
	Help       string
	MetricType string
	Stats      []MetricRecord
}

// MetricRecord - metric stats record
type MetricRecord struct {
	Value       float64
	Labels      []string
	LabelValues []string
}

// webCmd represents the web command
var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Run the exporter",
	Long: `With the command web you can start the hana sql exporter. For example:
	hana_sql_exporter web
	hana_sql_exporter web --config ./hana_sql_exporter.toml`,
	Run: func(cmd *cobra.Command, args []string) {
		config, err := getConfig()
		if err != nil {
			exit("Can't handle config file: ", err)
		}
		if config.Timeout == 0 {
			config.Timeout, err = cmd.Flags().GetUint("timeout")
			if err != nil {
				exit("Problem with timeout flag: ", err)
			}
		}
		if config.Ip == "" {
			config.Ip, err = cmd.Flags().GetString("ip")
			if err != nil {
				exit("Problem with ip flag: ", err)
			}
			if config.Ip == ""  {
				config.Ip = "127.0.0.1"
			}
		}
		if config.Port == "" {
			config.Port, err = cmd.Flags().GetString("port")
			if err != nil {
				exit("Problem with port flag: ", err)
			}
		}

		if config.LogLevel == "" {
			config.LogLevel, err = cmd.Flags().GetString("log-level")
			if err != nil {
				exit("Problem with log level: ", err)
			}
		}

		SetLogLevel(config.LogLevel)
		if config.LogFile == "" {
			config.LogFile, err = cmd.Flags().GetString("log-file")
			if err != nil {
				exit("Problem with log file: ", err)
			}
		}

		if config.LogFile != "" {
			f, err := os.OpenFile(config.LogFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				exit("error opening file: %v", err)
			}
			defer f.Close() // 确保文件最终会被关闭
			log.SetOutput(f)
		}

		// set data func
		config.DataFunc = config.GetMetricData
		config.QueryDataFunc = config.GetQueryMetricData

		err = config.Web()
		if err != nil {
			exit("Can't call exporter: ", err)
		}
	},
}

func init() {
	RootCmd.AddCommand(webCmd)

	webCmd.PersistentFlags().UintP("timeout", "t", 30, "scrape timeout of the hana_sql_exporter in seconds.")
	webCmd.PersistentFlags().StringP("ip", "i", "0.0.0.0", "ip, the hana_sql_exporter listens to.")
	webCmd.PersistentFlags().StringP("port", "p", "9888", "port, the hana_sql_exporter listens to.")
	webCmd.PersistentFlags().StringP("log-file", "l", "log.log", "logfile, the logfile location")
	webCmd.PersistentFlags().String("log-level", "error", "logfile, the log level")
}

// create new collector
func newCollector(stats func() []MetricData) *collector {
	return &collector{
		stats: stats,
	}
}

// Describe - describe implements prometheus.Collector.
func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(c, ch)
}

// Collect - implements prometheus.Collector.
func (c *collector) Collect(ch chan<- prometheus.Metric) {
	// take a stats snapshot. must be concurrency safe.
	stats := c.stats()

	var valueType = map[string]prometheus.ValueType{
		"gauge":   prometheus.GaugeValue,
		"counter": prometheus.CounterValue,
	}

	for _, mi := range stats {
		for _, v := range mi.Stats {
			m := prometheus.MustNewConstMetric(
				prometheus.NewDesc(mi.Name, mi.Help, v.Labels, nil),
				valueType[low(mi.MetricType)],
				v.Value,
				v.LabelValues...,
			)
			ch <- m
		}
	}
}

// HealthHandler - 健康检查接口
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "healthy")
}

// Web - start collector and web server
func (config *Config) Web() error {
	var err error

	log.Info("开始初始化HANA SQL Exporter服务")

	// 添加恢复机制
	// defer func() {
	// 	if r := recover(); r != nil {
	// 		log.WithField("panic", r).Error("服务发生严重错误，正在恢复")
	// 	}
	// }()

	config.Tenants, err = config.prepare()
	if err != nil {
		log.WithError(err).Error("租户准备失败")
		return errors.Wrap(err, "租户准备失败")
	}

	// close tenant connections at the end
	for i := range config.Tenants {
		defer config.Tenants[i].conn.Close()
	}

	// // 设置数据采集函数
	// config.DataFunc = config.GetMetricData
	// config.QueryDataFunc = config.GetQueryMetricData

	stats := func() []MetricData {
		start := time.Now()
		log.Debug("开始收集指标数据")

		// 使用带超时的上下文控制
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.Timeout)*time.Second)
		defer cancel()

		// 创建错误通道
		// errChan := make(chan error, 1)

		// 创建结果通道
		resultChan := make(chan []MetricData, 1)

		go func() {
			// 使用通道并发收集指标数据
			metricChan := make(chan []MetricData, 2)

			// 并发收集单指标和多指标数据
			go func() {
				metrics := config.CollectMetrics()
				metricChan <- metrics
			}()

			go func() {
				queryMetrics := config.CollectQueryMetrics()
				metricChan <- queryMetrics
			}()

			// 等待两个收集过程完成
			var allMetrics []MetricData
			existingMetrics := make(map[string]struct{})

			// 处理收集到的指标数据
			for i := 0; i < 2; i++ {
				metrics := <-metricChan
				
				// 检查并合并指标
				for _, m := range metrics {
					if _, exists := existingMetrics[m.Name]; exists {
						log.WithFields(log.Fields{
							"metric": m.Name,
						}).Warn("跳过重复的指标名称")
						continue
					}
					
					allMetrics = append(allMetrics, m)
					existingMetrics[m.Name] = struct{}{}
				}
			}


			select {
			case <-ctx.Done():
				return
			case resultChan <- allMetrics:
			}
		}()

		// 等待结果或超时
		select {
		case <-ctx.Done():
			log.Error("指标收集超时")
			return []MetricData{}
		case result := <-resultChan:
			duration := time.Since(start)
			log.WithFields(log.Fields{
				"metrics_count": len(result),
				"duration_ms":   duration.Milliseconds(),
			}).Info("指标数据收集完成")
			return result
		}
	}

	// start collector
	log.Info("注册Prometheus收集器")
	c := newCollector(stats)
	prometheus.MustRegister(c)
	if !log.IsLevelEnabled(log.DebugLevel) {
		prometheus.Unregister(collectors.NewGoCollector())
		prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}

	// 设置采集处理器选项
	handlerOpts := promhttp.HandlerOpts{
		MaxRequestsInFlight: 10, // 限制并发请求数
		Timeout:             time.Duration(config.Timeout) * time.Second,
		EnableOpenMetrics:   true,
	}
	handler := promhttp.HandlerFor(prometheus.DefaultGatherer, handlerOpts)

	// start http server
	log.WithField("ip", config.Ip).WithField("port", config.Port).Info("启动HTTP服务器")
	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)
	mux.HandleFunc("/health", HealthHandler) // 添加健康检查接口
	mux.HandleFunc("/", RootHandler)

	server := &http.Server{
		Addr:         config.Ip + ":" + config.Port,
		Handler:      mux,
		WriteTimeout: time.Duration(config.Timeout+2) * time.Second,
		ReadTimeout:  time.Duration(config.Timeout+2) * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 优雅关闭服务
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Info("接收到关闭信号，正在优雅关闭服务...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.WithError(err).Error("服务关闭出错")
		}
	}()

	log.WithFields(log.Fields{
		"address": server.Addr,
		"timeout": config.Timeout,
	}).Info("HTTP服务器配置完成，开始监听")

	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.WithError(err).Error("HTTP服务器启动失败")
		return errors.Wrap(err, "web(ListenAndServe)")
	}
	log.WithFields(log.Fields{
		"url": fmt.Sprintf("http://%s:%s", config.Ip,config.Port),
	}).Info("服务启动成功，可以通过以下地址访问")
	fmt.Printf("服务启动成功，可以通过以下地址访问: http://%s:%s\n", config.Ip,config.Port)
	return nil
}

// RootHandler - message, when calling mithout /metrics
func RootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "prometheus hana_sql_exporter: please call <host>:<port>/metrics")
}

// CollectMetrics - collecting all metrics and fetch the results
func (config *Config) CollectMetrics() []MetricData {
	var wg sync.WaitGroup
	metricCnt := len(config.Metrics)
	metricsC := make(chan MetricData, metricCnt)
	errC := make(chan error, metricCnt)

	// 创建采集任务
	for mPos := range config.Metrics {
		wg.Add(1)
		go func(mPos int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.WithFields(log.Fields{
						"metric": config.Metrics[mPos].Name,
						"panic":  r,
					}).Error("指标采集发生严重错误")
					errC <- fmt.Errorf("metric %s panic: %v", config.Metrics[mPos].Name, r)
				}
			}()

			var stats []MetricRecord

			stats = config.CollectMetric(mPos)

			if len(stats) == 0 {
				log.WithFields(log.Fields{
					"metric": config.Metrics[mPos].Name,
				}).Error("指标采集失败")
				errC <- fmt.Errorf("metric %s failed", config.Metrics[mPos].Name)
				return
			}

			metricsC <- MetricData{
				Name:       getMetricNameWithUnit(config.Metrics[mPos].Name, config.Metrics[mPos].Unit),
				Help:       config.Metrics[mPos].Help,
				MetricType: config.Metrics[mPos].MetricType,
				Stats:      stats,
			}
		}(mPos)
	}

	// 等待所有任务完成
	go func() {
		wg.Wait()
		close(metricsC)
		close(errC)
	}()

	// 收集结果和错误
	var metricsData []MetricData
	var errors []error

	for i := 0; i < metricCnt; i++ {
		select {
		case metric := <-metricsC:
			if metric.Stats != nil && len(metric.Stats) > 0 {
				metricsData = append(metricsData, metric)
			}
		case err := <-errC:
			if err != nil {
				errors = append(errors, err)
			}
		}
	}

	// 记录采集结果统计
	log.WithFields(log.Fields{
		"total_metrics":      metricCnt,
		"successful_metrics": len(metricsData),
		"failed_metrics":     len(errors),
	}).Info("指标采集完成")

	return metricsData
}

// CollectMetric - collecting one metric for every tenants
func (config *Config) CollectMetric(mPos int) []MetricRecord {

	// set timeout
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Duration(config.Timeout)*time.Second))
	defer cancel()

	tenantCnt := len(config.Tenants)
	metricC := make(chan []MetricRecord, tenantCnt)

	// 添加WaitGroup来等待所有goroutine完成
	var wg sync.WaitGroup

	for tPos := range config.Tenants {
		wg.Add(1)
		go func(tPos int) {
			defer wg.Done()
			metricC <- config.DataFunc(mPos, tPos)
		}(tPos)
	}

	// 在单独的goroutine中等待所有任务完成并关闭通道
	// 等待所有goroutine完成
	go func() {
		wg.Wait()
		close(metricC)
	}()
	// collect data
	var sData []MetricRecord
	for mc := range metricC {
		select {
		case <-ctx.Done():
			return sData
		default:
			if mc != nil {
				sData = append(sData, mc...)
			}
		}
	}
	return sData
}

// GetMetricData - metric data for one tenant
func (config *Config) GetMetricData(mPos, tPos int) []MetricRecord {
	m := config.Metrics[mPos]
	if m.Disabled {
		return nil
	}
	start := time.Now()
	logFields := log.Fields{
		"metric": config.Metrics[mPos].Name,
		"tenant": config.Tenants[tPos].Name,
	}

	// 获取所有匹配的schema
	var matchedSchemas []string
	for _, schema := range config.Metrics[mPos].SchemaFilter {
		if ContainsString(schema, config.Tenants[tPos].Schemas) {
			matchedSchemas = append(matchedSchemas, schema)
		}
	}

	if len(matchedSchemas) == 0 {
		log.WithFields(logFields).Error("metrics schema filter must include at least one tenant schema")
		return nil
	}

	var allMetrics []MetricRecord
	var errors []error

	// 遍历所有匹配的schema执行查询
	for _, schema := range matchedSchemas {
		schemaLogFields := log.Fields{
			"metric": config.Metrics[mPos].Name,
			"tenant": config.Tenants[tPos].Name,
			"schema": schema,
		}

		// 替换SQL中的schema占位符
		sel := strings.ReplaceAll(config.Metrics[mPos].SQL, "<SCHEMA>", schema)
		log.WithFields(schemaLogFields).WithField("sql", sel).Debug("执行SQL查询")


		// 设置查询超时
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.Timeout)*time.Second)
		rows, err := config.Tenants[tPos].conn.QueryContext(ctx, sel)

		if err != nil {
			cancel()
			log.WithFields(schemaLogFields).WithError(err).WithField("sql", sel).Error("数据读取失败")
			errors = append(errors, fmt.Errorf("schema %s data read failed: %v", schema, err))
			continue
		}

		data, cols, err := config.Tenants[tPos].RowsConvert(rows)
		if err != nil {
			cancel()
			log.WithFields(schemaLogFields).WithError(err).Error("数据转换处理失败")
			errors = append(errors, fmt.Errorf("schema %s convert results failed: %v", schema, err))
			continue
		}
		cancel()
		// 处理查询结果
		md, err := config.Tenants[tPos].GetMetricRows(config.Metrics[mPos].Name, data, cols, config.Metrics[mPos].Labels, config.Metrics[mPos].ValueColumn)
		if err != nil {
			log.WithFields(schemaLogFields).WithError(err).Error("处理查询结果失败")
			errors = append(errors, fmt.Errorf("schema %s process results failed: %v", schema, err))
			continue
		}

		// 更新schema标签
		for i := range md {
			for j, label := range md[i].Labels {
				if label == "schema" {
					md[i].LabelValues[j] = low(schema)
					break
				}
			}
		}

		// 自动添加unit标签会导致在grafana中无法合并多个指标，所以暂时不自动添加unit标签，如果需要单元信息，在grafana中手动添加
		// 比如：同时进行指标的计数与求和，使用merge功能合并时，因为存在多个unit标签，无法合并在同一个table中显示。
		// 指标名称本身就带有单位信息，所以不需要再添加unit标签

		// 如果unit不为空，添加unit标签
		// if config.Metrics[mPos].Unit != "" {
		// 	for i := range md {
		// 		// 检查是否已存在unit标签
		// 		unitLabelExists := false
		// 		for _, label := range md[i].Labels {
		// 			if low(label) == "unit" {
		// 				unitLabelExists = true
		// 				break
		// 			}
		// 		}
		// 		if !unitLabelExists {
		// 			md[i].Labels = append(md[i].Labels, "unit")
		// 			md[i].LabelValues = append(md[i].LabelValues, low(config.Metrics[mPos].Unit))
		// 		}
		// 	}
		// }

		allMetrics = append(allMetrics, md...)
	}

	duration := time.Since(start)
	log.WithFields(log.Fields{
		"metric":       config.Metrics[mPos].Name,
		"tenant":       config.Tenants[tPos].Name,
		"schemas":      len(matchedSchemas),
		"rows_count":   len(allMetrics),
		"errors_count": len(errors),
		"duration_ms":  duration.Milliseconds(),
	}).Info("指标数据采集完成")

	if len(errors) > 0 {
		log.WithFields(logFields).WithField("errors", errors).Error("部分schema查询失败")
	}

	return allMetrics
}

// GetSelection - prepare the db selection
func (config *Config) GetSelection(mPos, tPos int) string {
	// 检查版本要求
	if config.Metrics[mPos].VersionFilter != "" {
		version, err := config.GetHanaVersion(tPos)
		if err != nil {
			log.WithFields(log.Fields{
				"metric": config.Metrics[mPos].Name,
				"tenant": config.Tenants[tPos].Name,
				"error":  err,
			}).Error("获取数据库版本失败")
			return ""
		}
		if !config.CheckVersionRequirement(version, config.Metrics[mPos].VersionFilter) {
			log.WithFields(log.Fields{
				"metric":      config.Metrics[mPos].Name,
				"tenant":      config.Tenants[tPos].Name,
				"version":     version,
				"requirement": config.Metrics[mPos].VersionFilter,
			}).Debug("数据库版本不满足要求，跳过该指标")
			return ""
		}
	}

	// all values of metrics tag filter must be in tenants tags, otherwise the
	// metric is not relevant for the tenant
	if !SubSliceInSlice(config.Metrics[mPos].TagFilter, config.Tenants[tPos].Tags) {
		return ""
	}

	sel := strings.TrimSpace(config.Metrics[mPos].SQL)
	if !strings.EqualFold(sel[0:6], "select") {
		log.WithFields(log.Fields{
			"metric": config.Metrics[mPos].Name,
			"tenant": config.Tenants[tPos].Name,
		}).Error("Only selects are allowed")
		return ""
	}

	// 获取所有匹配的schema
	var matchedSchemas []string
	for _, schema := range config.Metrics[mPos].SchemaFilter {
		if ContainsString(schema, config.Tenants[tPos].Schemas) {
			matchedSchemas = append(matchedSchemas, schema)
		}
	}

	if len(matchedSchemas) == 0 {
		log.WithFields(log.Fields{
			"metric": config.Metrics[mPos].Name,
			"tenant": config.Tenants[tPos].Name,
		}).Error("metrics schema filter must include at least one tenant schema")
		return ""
	}

	// 使用第一个匹配的schema作为SQL查询
	return strings.ReplaceAll(config.Metrics[mPos].SQL, "<SCHEMA>", matchedSchemas[0])
}

func (tenent *TenantInfo) RowsConvert(rows *sql.Rows) ([][]interface{}, []string, error) {
	//exact the sql.Rows out
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, errors.Wrap(err, "GetMetricRows(rows.Columns)")
	}
	if len(cols) < 1 {
		return nil, nil, errors.New("GetMetricRows(no columns)")
	}

	rows1 := make([][]interface{}, 0)

	for rows.Next() {
		// 创建通用接口切片用于扫描
		values := make([]interface{}, len(cols))
		for i := range values {
			// 为每列创建一个通用接口
			values[i] = new(interface{})
		}
		err = rows.Scan(values...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "GetMetricRows(rows.Scan)")
		}
		rows1 = append(rows1, values)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, errors.Wrap(err, "GetMetricRows(rows)")
	}
	return rows1, cols, nil
}

// GetMetricRows - return the metric values
func (tenant *TenantInfo) GetMetricRows(metricName string, rows [][]interface{}, cols []string, labels []string, valueColumn string) ([]MetricRecord, error) {
	label_search := ""
	if len(labels) > 0 {
		label_search = low(strings.Join(labels, ","))
	}

	if len(cols) < 1 {
		return nil, errors.New("GetMetricRows(no columns)")
	}

	// 确定值列的索引
	valueColumnIndex := 0
	if valueColumn != "" {
		for i, col := range cols {
			if strings.EqualFold(col, valueColumn) {
				valueColumnIndex = i
				break
			}
		}
	}

	meta := tenant.Config.getSharedMetaData(tenant.Index)

	var md []MetricRecord
	for _, values := range rows {
		data := MetricRecord{
			Labels:      append([]string{"tenant", "usage", "schema"}, meta.Labels...),
			LabelValues: append([]string{low(tenant.Name), low(tenant.Usage), ""}, meta.LabelValues...),
		}
		for i := range values {
			// 检查空值
			if values[i] == nil || *(values[i].(*interface{})) == nil {
				continue
			}

			if i == valueColumnIndex {
				// 处理值列
				val := *(values[i].(*interface{}))
				switch v := val.(type) {
				case time.Time:
					// 处理TIMESTAMP类型
					data.Value = float64(v.Unix())
				case string:
					// 尝试解析为时间戳或数值
					if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
						data.Value = float64(t.Unix())
					} else {
						data.Value, err = parseFractionToFloat(v)
						if err != nil {
							log.WithFields(log.Fields{
								"error":  err,
								"type":   "string",
								"value":  v,
								"metric": metricName,
							}).Warn("GetMetricRows: 字符串值无法转换为浮点数，使用默认值0")
							data.Value = 0
						}
					}
				default:
					// 尝试转换为float64
					if fVal, err := convertToFloat64(v); err == nil {
						data.Value = fVal
					} else {
						data.Value = 0
						
							log.WithFields(log.Fields{
								"error":  err,
								"type":   fmt.Sprintf("%T", v),
								"value":  v,
								"metric": metricName,
							}).Warn("GetMetricRows: 不支持的值类型，使用默认值0")
						
					}
				}
			} else {
				// 处理标签列
				strVal := convertToString(*(values[i].(*interface{})))
					if len(labels) > 0 {
						if strings.Contains(label_search, low(cols[i])) {
							// 检查是否已存在该标签
							labelExists := false
							for _, existingLabel := range data.Labels {
								if existingLabel == low(cols[i]) {
									labelExists = true
									break
								}
							}
							if !labelExists {
								data.Labels = append(data.Labels, low(cols[i]))
								data.LabelValues = append(data.LabelValues, low(strings.Join(strings.Split(strVal, " "), "_")))
							}
						}
					} else {
						// 检查是否已存在该标签
						labelExists := false
						for _, existingLabel := range data.Labels {
							if existingLabel == low(cols[i]) {
								labelExists = true
								break
							}
						}
						if !labelExists {
							data.Labels = append(data.Labels, low(cols[i]))
							data.LabelValues = append(data.LabelValues, low(strings.Join(strings.Split(strVal, " "), "_")))
						}
					}
			}
		}
		md = append(md, data)
	}

	return md, nil
}

// add missing information to tenant struct
func (config *Config) prepare() ([]TenantInfo, error) {
	log.Info("开始准备租户连接和信息收集")
	var tenantsOk []TenantInfo

	// 初始化版本缓存
	// config.versionMutex.Lock()
	// config.versionCache = make(map[int]string)
	// config.versionMutex.Unlock()

	// adapt config.Metrics schema filter
	config.AdaptSchemaFilter()

	secretMap, err := config.GetSecretMap()
	if err != nil {
		log.WithError(err).Error("获取密钥映射失败")
		return nil, errors.Wrap(err, "prepare(getSecretMap)")
	}

	for i := 0; i < len(config.Tenants); i++ {
		log.WithFields(log.Fields{
			"tenant":   config.Tenants[i].Name,
			"conn_str": config.Tenants[i].ConnStr,
		}).Info("尝试建立租户数据库连接")

		config.Tenants[i].conn = config.getConnection(i, secretMap)
		if config.Tenants[i].conn == nil {
			log.WithField("tenant", config.Tenants[i].Name).Error("建立数据库连接失败，跳过该租户")
			continue
		}

		// get tenant usage and hana-user schema information
		log.WithField("tenant", config.Tenants[i].Name).Debug("开始收集租户使用信息和schema权限")
		err = config.collectRemainingTenantInfos(i)
		if err != nil {
			log.WithFields(log.Fields{
				"tenant": config.Tenants[i].Name,
				"error":  err,
			}).Error("收集租户信息失败 - 租户将被移除")
			continue
		}

		// 获取并缓存版本信息
		// version, err := config.getHanaVersionFromDB(i)
		// if err != nil {
		// 	log.WithFields(log.Fields{
		// 		"tenant": config.Tenants[i].Name,
		// 		"error":  err,
		// 	}).Error("获取数据库版本失败")
		// 	continue
		// }
		err = config.retrieveMetadata(i)
		if err != nil {
			log.WithFields(log.Fields{
				"tenant": config.Tenants[i].Name,
				"error":  err,
			}).Error("获取数据库元数据失败")
			continue
		}
		// config.versionMutex.Lock()
		// config.versionCache[i] = version
		// config.versionMutex.Unlock()

		log.WithFields(log.Fields{
			"tenant":  config.Tenants[i].Name,
			"usage":   config.Tenants[i].Usage,
			"schemas": len(config.Tenants[i].Schemas),
		}).Info("租户元信息收集完成")

		config.Tenants[i].Config = config
		config.Tenants[i].Index = i
		tenantsOk = append(tenantsOk, config.Tenants[i])
	}

	log.WithField("total_tenants", len(tenantsOk)).Info("租户准备完成")
	return tenantsOk, nil
}

// get tenant usage and hana-user schema information
func (config *Config) collectRemainingTenantInfos(tPos int) error {

	// get tenant usage information
	row := config.Tenants[tPos].conn.QueryRow("select usage from sys.m_database")
	err := row.Scan(&config.Tenants[tPos].Usage)
	if err != nil {
		return errors.Wrap(err, "collectRemainingTenantInfos(Scan)")
	}

	// append sys schema to tenant schemas
	config.Tenants[tPos].Schemas = append(config.Tenants[tPos].Schemas, "sys")

	// append remaining user schema privileges
	rows, err := config.Tenants[tPos].conn.Query("select schema_name from sys.granted_privileges where object_type='SCHEMA' and grantee=$1", strings.ToUpper(config.Tenants[tPos].User))
	if err != nil {
		return errors.Wrap(err, "collectRemainingTenantInfos(Query)")
	}

	for rows.Next() {
		var schema string
		err := rows.Scan(&schema)
		if err != nil {
			return errors.Wrap(err, "collectRemainingTenantInfos(Scan)")
		}
		config.Tenants[tPos].Schemas = append(config.Tenants[tPos].Schemas, schema)
	}
	if err = rows.Err(); err != nil {
		return errors.Wrap(err, "collectRemainingTenantInfos(rows.Err)")
	}
	return nil
}

// AdaptSchemaFilter - add sys schema to SchemaFilter if it does not exists
func (config *Config) AdaptSchemaFilter() {

	for mPos := range config.Metrics {
		if len(config.Metrics[mPos].SchemaFilter) == 0 {
			config.Metrics[mPos].SchemaFilter = append(config.Metrics[mPos].SchemaFilter, "sys")
		}
	}
}

// ContainsString - true, if slice contains string
func ContainsString(str string, slice []string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, str) {
			return true
		}
	}
	return false
}

// SubSliceInSlice - true, if every item in sublice exists in slice or sublice is empty
func SubSliceInSlice(subSlice []string, slice []string) bool {
	for _, vs := range subSlice {
		for _, v := range slice {
			if strings.EqualFold(vs, v) {
				goto nextCheck
			}
		}
		return false
	nextCheck:
	}
	return true
}

// FirstValueInSlice - return first sublice value that exists in slice
func FirstValueInSlice(subSlice []string, slice []string) string {
	for _, vs := range subSlice {
		for _, v := range slice {
			if strings.EqualFold(vs, v) {
				return vs
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------

// GetTestData1 - for testing purpose only
func (config *Config) GetTestData1(mPos, tPos int) []MetricRecord {
	mr := []MetricRecord{
		{
			999.0,
			[]string{"l" + strconv.Itoa(mPos) + strconv.Itoa(tPos)},
			[]string{"lv" + strconv.Itoa(mPos) + strconv.Itoa(tPos)},
		},
	}
	return mr
}

// GetTestData2 - for testing purpose only
func (config *Config) GetTestData2(mPos, tPos int) []MetricRecord {
	return nil
}

// CollectQueryMetrics - 收集所有多指标查询的结果
func (config *Config) CollectQueryMetrics() []MetricData {
	var wg sync.WaitGroup
	queryCnt := len(config.Queries)
	queriesC := make(chan []MetricData, queryCnt)

	for qPos := range config.Queries {
		wg.Add(1)
		go func(qPos int) {
			defer wg.Done()
			queriesC <- config.CollectQueryMetric(qPos)
		}(qPos)
	}

	go func() {
		wg.Wait()
		close(queriesC)
	}()

	var metricsData []MetricData
	for query := range queriesC {
		if query != nil {
			metricsData = append(metricsData, query...)
		}
	}

	return metricsData
}

// CollectQueryMetric - 为每个租户收集一个查询的多个指标
func (config *Config) CollectQueryMetric(qPos int) []MetricData {
	// 设置超时
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Duration(config.Timeout)*time.Second))
	defer cancel()

	tenantCnt := len(config.Tenants)
	metricC := make(chan []MetricData, tenantCnt)
	var wg sync.WaitGroup
	for tPos := range config.Tenants {
		wg.Add(1)
		go func(tPos int) {
			defer wg.Done()

			metricC <- config.QueryDataFunc(qPos, tPos)
		}(tPos)
	}
	go func() {
		wg.Wait()
		close(metricC)
	}()
	// 收集数据
	var sData []MetricData
	for mc := range metricC {
		select {
		case <-ctx.Done():
			return sData
		default:
			if mc != nil {
				sData = append(sData, mc...)
			}
		}
	}
	return sData
}

// GetQueryMetricData - 为一个租户获取查询的多个指标数据
func (config *Config) GetQueryMetricData(qPos, tPos int) []MetricData {
	if config.Queries[qPos].Disabled {
		return nil
	}

	// 检查所有子指标是否都被禁用
	allDisabled := true
	for _, metric := range config.Queries[qPos].Metrics {
		if !metric.Disabled {
			allDisabled = false
			break
		}
	}
	if allDisabled {
		log.WithFields(log.Fields{
			"query":   config.Queries[qPos].SQL,
			"tenant":  config.Tenants[tPos].Name,
			"metrics": len(config.Queries[qPos].Metrics),
		}).Info("跳过执行查询，所有子指标已禁用")
		return nil
	}

	start := time.Now()
	logFields := log.Fields{
		"query":  config.Queries[qPos].SQL,
		"tenant": config.Tenants[tPos].Name,
	}

	// 获取所有匹配的schema
	var matchedSchemas []string
	if len(config.Queries[qPos].SchemaFilter) == 0 {
		config.Queries[qPos].SchemaFilter = []string{"sys"}
	}

	for _, schema := range config.Queries[qPos].SchemaFilter {
		if ContainsString(schema, config.Tenants[tPos].Schemas) {
			matchedSchemas = append(matchedSchemas, schema)
		}
	}

	if len(matchedSchemas) == 0 {
		log.WithFields(logFields).Error("query schema filter must include at least one tenant schema")
		return nil
	}

	var allMetrics []MetricData
	// 遍历所有匹配的schema执行查询
	for _, schema := range matchedSchemas {
		// 替换SQL中的schema占位符
		sel := strings.ReplaceAll(config.Queries[qPos].SQL, "<SCHEMA>", schema)
		log.WithFields(logFields).WithField("schema", schema).WithField("sql", sel).Debug("执行SQL查询")

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.Timeout)*time.Second)

		rows, err := config.Tenants[tPos].conn.QueryContext(ctx, sel)

		// rows, err := config.Tenants[tPos].conn.Query(sel)
		if err != nil {
			cancel()
			log.WithFields(logFields).WithField("schema", schema).WithField("sql", sel).WithError(err).Error("执行SQL查询失败")
			continue
		}
		data, cols, err := config.Tenants[tPos].RowsConvert(rows)
		if err != nil {
			cancel()
			log.WithFields(logFields).WithField("schema", schema).WithError(err).Error("数据转换处理失败")
			continue
		}
		cancel()
		// 处理查询结果
		var metricsData []MetricData
		for _, metric := range config.Queries[qPos].Metrics {
			if metric.Disabled {
				continue
			}
			metricData := MetricData{
				Name:       getMetricNameWithUnit(metric.Name, metric.Unit),
				Help:       metric.Help,
				MetricType: metric.MetricType,
			}

			md, err := config.Tenants[tPos].GetMetricRows(metric.Name, data, cols, metric.Labels, metric.ValueColumn)
			if err != nil {
				log.WithFields(logFields).WithField("schema", schema).WithError(err).Error("处理查询结果失败")
				continue
			}

			// 更新schema标签
			for i := range md {
				for j, label := range md[i].Labels {
					if label == "schema" {
						md[i].LabelValues[j] = low(schema)
						break
					}
				}
			}

			// 自动添加unit标签会导致在grafana中无法合并多个指标，所以暂时不自动添加unit标签，如果需要单元信息，在grafana中手动添加
			// 比如：同时进行指标的计数与求和，使用merge功能合并时，因为存在多个unit标签，无法合并在同一个table中显示。
			// 指标名称本身就带有单位信息，所以不需要再添加unit标签

			// 如果unit不为空，添加unit标签
			// if metric.Unit != "" {
			// 	for i := range md {
			// 		// 检查是否已存在unit标签
			// 		unitLabelExists := false
			// 		for _, label := range md[i].Labels {
			// 			if low(label) == "unit" {
			// 				unitLabelExists = true
			// 				break
			// 			}
			// 		}
			// 		if !unitLabelExists {
			// 			md[i].Labels = append(md[i].Labels, "unit")
			// 			md[i].LabelValues = append(md[i].LabelValues, low(metric.Unit))
			// 		}
			// 	}
			// }

			metricData.Stats = append(metricData.Stats, md...)
			if len(metricData.Stats) > 0 {
				metricsData = append(metricsData, metricData)
			}
		}
		allMetrics = append(allMetrics, metricsData...)
	}

	duration := time.Since(start)
	log.WithFields(log.Fields{
		"query":       config.Queries[qPos].SQL,
		"tenant":      config.Tenants[tPos].Name,
		"schemas":     len(matchedSchemas),
		"metrics":     len(allMetrics),
		"duration_ms": duration.Milliseconds(),
	}).Debug("查询数据收集完成")

	return allMetrics
}

// GetSelection - prepare the db selection for multi-metric query
func (config *Config) GetQuerySelection(qPos, tPos int) string {
	// 检查版本要求
	if config.Queries[qPos].VersionFilter != "" {
		version, err := config.GetHanaVersion(tPos)
		if err != nil {
			log.WithFields(log.Fields{
				"query":  config.Queries[qPos].SQL,
				"tenant": config.Tenants[tPos].Name,
				"error":  err,
			}).Error("获取数据库版本失败")
			return ""
		}
		if !config.CheckVersionRequirement(version, config.Queries[qPos].VersionFilter) {
			log.WithFields(log.Fields{
				"query":       config.Queries[qPos].SQL,
				"tenant":      config.Tenants[tPos].Name,
				"version":     version,
				"requirement": config.Queries[qPos].VersionFilter,
			}).Debug("数据库版本不满足要求，跳过该查询")
			return ""
		}
	}

	// all values of query tag filter must be in tenants tags
	if !SubSliceInSlice(config.Queries[qPos].TagFilter, config.Tenants[tPos].Tags) {
		return ""
	}

	sel := strings.TrimSpace(config.Queries[qPos].SQL)
	if !strings.EqualFold(sel[0:6], "select") {
		log.WithFields(log.Fields{
			"query":  config.Queries[qPos].SQL,
			"tenant": config.Tenants[tPos].Name,
		}).Error("Only selects are allowed")
		return ""
	}

	if len(config.Queries[qPos].SchemaFilter) == 0 {
		config.Queries[qPos].SchemaFilter = []string{"sys"}
	}
	// 获取所有匹配的schema
	var matchedSchemas []string
	for _, schema := range config.Queries[qPos].SchemaFilter {
		if ContainsString(schema, config.Tenants[tPos].Schemas) {
			matchedSchemas = append(matchedSchemas, schema)
		}
	}

	if len(matchedSchemas) == 0 {
		log.WithFields(log.Fields{
			"query":  config.Queries[qPos].SQL,
			"tenant": config.Tenants[tPos].Name,
		}).Error("query schema filter must include at least one tenant schema")
		return ""
	}

	// 使用第一个匹配的schema作为SQL查询
	return strings.ReplaceAll(config.Queries[qPos].SQL, "<SCHEMA>", matchedSchemas[0])
}

// GetHanaVersion - 获取SAP HANA数据库版本
func (config *Config) GetHanaVersion(tPos int) (string, error) {
	// 从缓存中获取版本信息
	// config.versionMutex.RLock()
	version := config.Tenants[tPos].Version
	// config.versionMutex.RUnlock()

	if version == "" {
		return "", errors.New("version information not found in cache")
	}

	return version, nil
}

// func (config *Config) getHanaVersionFromDB(tPos int) (string, error) {
// 	row := config.Tenants[tPos].conn.QueryRow("SELECT value FROM SYS.M_HOST_INFORMATION where key = 'build_version'")
// 	var version string
// 	err := row.Scan(&version)
// 	if err != nil {
// 		return "", errors.Wrap(err, "getHanaVersionFromDB(Scan)")
// 	}
// 	return version, nil
// }

// CheckVersionRequirement - 检查版本是否满足要求
func (config *Config) CheckVersionRequirement(version, requirement string) bool {
	// 解析版本要求
	req := strings.TrimSpace(requirement)
	if req == "" {
		return true
	}

	// 分割多个条件
	conditions := strings.Split(req, " ")

	for _, cond := range conditions {
		cond = strings.TrimSpace(cond)
		if cond == "" {
			continue
		}

		// 解析单个条件
		var op string
		var reqVersion string
		if strings.HasPrefix(cond, ">=") {
			op = ">="
			reqVersion = strings.TrimSpace(cond[2:])
		} else if strings.HasPrefix(cond, "<=") {
			op = "<="
			reqVersion = strings.TrimSpace(cond[2:])
		} else if strings.HasPrefix(cond, ">") {
			op = ">"
			reqVersion = strings.TrimSpace(cond[1:])
		} else if strings.HasPrefix(cond, "<") {
			op = "<"
			reqVersion = strings.TrimSpace(cond[1:])
		} else if strings.HasPrefix(cond, "=") {
			op = "="
			reqVersion = strings.TrimSpace(cond[1:])
		} else {
			op = "="
			reqVersion = cond
		}

		// 验证单个条件
		switch op {
		case ">=":
			if !(version >= reqVersion) {
				return false
			}
		case "<=":
			if !(version <= reqVersion) {
				return false
			}
		case ">":
			if !(version > reqVersion) {
				return false
			}
		case "<":
			if !(version < reqVersion) {
				return false
			}
		case "=":
			if version != reqVersion {
				return false
			}
		default:
			return false
		}
	}

	return true
}

func parseFractionToFloat(value string) (float64, error) {
	// 去除前导和尾随空格
	value = strings.TrimSpace(value)
	
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		// 处理分数形式
		numerator, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			return 0, err
		}
		denominator, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return 0, err
		}
		if denominator == 0 {
			return 0, fmt.Errorf("除数不能为零")
		}
		return numerator / denominator, nil
	}
	
	// 处理普通数值
	return strconv.ParseFloat(value, 64)
}

// 辅助函数：将任意类型转换为float64
func convertToFloat64(v interface{}) (float64, error) {
	switch value := v.(type) {
	case float64:
		return value, nil
	case float32:
		return float64(value), nil
	case int64:
		return float64(value), nil
	case int32:
		return float64(value), nil
	case int:
		return float64(value), nil
	case uint64:
		return float64(value), nil
	case uint32:
		return float64(value), nil
	case uint:
		return float64(value), nil
	case []uint8:
		return parseFractionToFloat(string(value))
	case string:
		return parseFractionToFloat(value)
	case *big.Rat:
		// 处理big.Rat类型
		f, _ := value.Float64()
		return f, nil
	default:
		// 尝试将其他类型转换为字符串后再解析
		strVal := fmt.Sprintf("%v", value)
		return parseFractionToFloat(strVal)
	}
}

// 辅助函数：将任意类型转换为字符串
func convertToString(v interface{}) string {
	switch value := v.(type) {
	case string:
		return value
	case time.Time:
		return value.Format("2006-01-02 15:04:05")
	case *big.Rat:
		// 对于big.Rat类型，使用String()方法获取字符串表示
		return value.String()
	case []uint8:
		// 对于[]uint8类型，直接转换为字符串
		return string(value)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// retrieveMetadata 获取数据库元数据并填充到TenantInfo
func (config *Config) retrieveMetadata(tId int) error {
	query := `SELECT
(SELECT value FROM M_SYSTEM_OVERVIEW WHERE section = 'System' AND name = 'Instance ID') SID,
(SELECT value FROM M_SYSTEM_OVERVIEW WHERE section = 'System' AND name = 'Instance Number') INSNR,
m.database_name,
m.version
FROM m_database m`

	row := config.Tenants[tId].conn.QueryRow(query)
	err := row.Scan(
		&config.Tenants[tId].SID,
		&config.Tenants[tId].InstanceNumber,
		&config.Tenants[tId].DatabaseName,
		&config.Tenants[tId].Version,
	)
	if err != nil {
		log.WithFields(log.Fields{
			"tenant": config.Tenants[tId].Name,
			"error":  err,
		}).Error("获取数据库元数据失败")
		return err
	}
	return nil
}

// 获取共享的元数据标签记录
func (config *Config) getSharedMetaData(tId int) MetricRecord {
	return MetricRecord{
		Labels: []string{"sid", "insnr", "database_name"},
		LabelValues: []string{
			config.Tenants[tId].SID,
			config.Tenants[tId].InstanceNumber,
			config.Tenants[tId].DatabaseName,
		},
	}
}

func getMetricNameWithUnit(name, unit string) string {
	if unit == "" {
		return name
	}
	suffix := "_" + strings.ToLower(unit)
	if strings.HasSuffix(strings.ToLower(name), suffix) {
		return name
	}
	return name + suffix
}
