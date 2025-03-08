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

// processVersionRange 处理HANA版本范围过滤条件
func processVersionRange(hanaVersionRange []string) string {
	if len(hanaVersionRange) == 2 {
		minVersion := hanaVersionRange[0]
		maxVersion := hanaVersionRange[1]

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

		if compare(minVersion, maxVersion) > 0 {
			minVersion, maxVersion = maxVersion, minVersion
		}
		log.Printf("Processing version range: %s - %s", minVersion, maxVersion)
		return fmt.Sprintf(">=%s <=%s", minVersion, maxVersion)
	} else if len(hanaVersionRange) == 1 {
		minVersion := hanaVersionRange[0]
		log.Printf("Minimum version requirement detected: %s", minVersion)
		return fmt.Sprintf(">=%s", minVersion)
	}

	log.Printf("Invalid HanaVersionRange: expected 1 or 2 elements, got %d", len(hanaVersionRange))
	return ""
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

		// 处理版本范围
		query.VersionFilter = processVersionRange(queryConfig.HanaVersionRange)

		// 转换指标
		for _, metric := range queryConfig.Metrics {
			tomlMetric := QueryMetricInfo{
				Name:        metric.Name,
				Help:        metric.Description,
				MetricType:  strings.ToLower(metric.Type),
				ValueColumn: metric.Value,
				Unit:        metric.Unit,
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
