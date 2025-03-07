package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml"
	"github.com/spf13/cobra"
)

var (
	inputFile  string
	outputFile string
)

// convertCmd represents the convert command
var convertCmd = &cobra.Command{
	Use:   "convert",
	Short: "Convert metrics JSON file to TOML format",
	Long: `Convert a metrics JSON configuration file to TOML format for HANA SQL exporter.
Example: hana_sql_exporter convert -i test/metrics.json -o test/hana_sql_exporter.toml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return convertMetrics(inputFile, outputFile)
	},
}

func init() {
	RootCmd.AddCommand(convertCmd)

	// Add flags for input and output files with default paths in test directory
	convertCmd.Flags().StringVarP(&inputFile, "input", "i", "test/metrics.json", "Input JSON file path")
	convertCmd.Flags().StringVarP(&outputFile, "output", "o", "test/hana_sql_exporter.toml", "Output TOML file path")
}

type Metric struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	Value       string   `json:"value"`
	Unit        string   `json:"unit"`
	Type        string   `json:"type"`
}

type QueryConfig struct {
	Enabled          bool      `json:"enabled"`
	HanaVersionRange []string  `json:"hana_version_range,omitempty"`
	Metrics          []*Metric `json:"metrics"`
}

// convertMetrics 将metrics.json转换为TOML格式
func convertMetrics(input, output string) error {
	jsonFile, err := os.Open(input)
	if err != nil {
		return fmt.Errorf("failed to open input file %s: %v", input, err)
	}
	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	// 使用map来解析JSON，其中key是SQL查询
	var jsonConfig map[string]QueryConfig
	err = json.Unmarshal(byteValue, &jsonConfig)
	if err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}

	// 创建TOML配置结构
	tomlConfig := Config{
		Queries: make([]QueryInfo, 0),
	}

	for sql, queryConfig := range jsonConfig {
		if !queryConfig.Enabled {
			continue
		}

		query := QueryInfo{
			SQL:     sql,
			Metrics: make([]QueryMetricInfo, 0),
		}

		// 添加版本范围（如果存在）
		// 在转换代码前添加检查逻辑
		if len(queryConfig.HanaVersionRange) >= 2 {
			// 提取并确保版本顺序正确
			minVersion := queryConfig.HanaVersionRange[0]
			maxVersion := queryConfig.HanaVersionRange[1]

			// 简易版本号比较（假设版本号为 x.x.x 格式）
			compare := func(v1, v2 string) int {
				v1Parts := strings.Split(v1, ".")
				v2Parts := strings.Split(v2, ".")
				for i := 0; i < len(v1Parts) && i < len(v2Parts); i++ {
					vp1, _ := strconv.Atoi(v1Parts[i])
					vp2, _ := strconv.Atoi(v2Parts[i])
					if vp1 > vp2 {
						return 1
					} else if vp1 < vp2 {
						return -1
					}
				}
				return 0
			}

			// 如果最小版本 > 最大版本，自动交换
			if compare(minVersion, maxVersion) > 0 {
				minVersion, maxVersion = maxVersion, minVersion
			}

			query.VersionFilter = fmt.Sprintf(">=%s <=%s", minVersion, maxVersion)
		} else {
			// 处理无效的版本范围配置
			log.Printf("Invalid HanaVersionRange: expected 2 elements, got %d",
				len(queryConfig.HanaVersionRange))
			// 可以设置为空或默认值
			query.VersionFilter = ""
		}

		// 转换指标
		for _, metric := range queryConfig.Metrics {
			tomlMetric := QueryMetricInfo{
				Name:        metric.Name,
				Help:        metric.Description,
				MetricType:  strings.ToLower(metric.Type),
				ValueColumn: metric.Value,
			}

			if len(metric.Labels) > 0 {
				tomlMetric.Labels = metric.Labels
			}

			query.Metrics = append(query.Metrics, tomlMetric)
		}

		tomlConfig.Queries = append(tomlConfig.Queries, query)
	}

	// 使用toml包直接写入文件
	tomlBytes, err := toml.Marshal(tomlConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal TOML: %v", err)
	}

	err = ioutil.WriteFile(output, tomlBytes, 0644)
	if err != nil {
		return fmt.Errorf("failed to write TOML file %s: %v", output, err)
	}

	fmt.Printf("Successfully converted %s to %s\n", input, output)
	return nil
}
