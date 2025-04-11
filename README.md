## SAP Hana SQL Exporter for Prometheus  [![CircleCI](https://circleci.com/gh/ulranh/hana_sql_exporter/tree/master.svg?style=svg)](https://circleci.com/gh/ulranh/hana_sql_exporter) [![Go Report Card](https://goreportcard.com/badge/github.com/ulranh/hana_sql_exporter)](https://goreportcard.com/report/github.com/ulranh/hana_sql_exporter)  [![Docker Pulls](https://img.shields.io/docker/pulls/ulranh/hana-sql-exporter)](https://hub.docker.com/r/ulranh/hana-sql-exporter)

The purpose of this exporter is to support monitoring SAP and SAP HanaDB  instances with [Prometheus](https://prometheus.io) and [Grafana](https://grafana.com).

## Installation

The exporter can be downloaded as [released Version](https://github.com/ulranh/hana_sql_exporter/releases/latest). To build the current version you need the [Go](https://golang.org/) programming language:


```
$ git clone git@github.com:ulranh/hana_sql_exporter.git
$ cd hana_sql_exporter
$ go build
```
## Preparation

#### Database User
A database user is necessary for every tenant with read access for all affected schemas:

```
# Login with authorized user:
$ create user <user> password <pw> no force_first_password_change;
$ alter user <user> disable password lifetime;
$ grant catalog read to <user>;

# Login with authorized user:
$ grant select on schema <schema> to <user>;
# <schema>: _SYS_STATISTICS, SAPABAP1, SAPHANADB ... 
```

#### Configfile
The next necessary piece is a [toml](https://github.com/toml-lang/toml) configuration file where the encrypted passwords, the tenant- and metric-information are stored. The expected default name is .hana_sql_exporter.toml and the expected default location of this file is the users home directory. The flag --config (-c) can be used to assign other locations or names.

The file contains a Tenants slice followed by a Metrics Slice:

```
[[Tenants]]
  Name = "q01"
  Tags = ["abap", "ewm"]
  ConnStr = "hanaq01.example.com:32041"
  User = "dbuser1"

[[Tenants]]
  Name = "q02"
  Tags = ["abap", "erp"]
  ConnStr = "hanaqj1.example.com:31044"
  User = "dbuser2"

[[Metrics]]
  Name = "hdb_backup_status"
  Help = "Status of last hana backup."
  MetricType = "gauge"
  TagFilter = []
  SchemaFilter = [] # the sys schema will be added automatically
  SQL = "select (case when state_name = 'successful' then 0 when state_name = 'running' then 1 else -1 end) as val, entry_type_name as type from <SCHEMA>.m_backup_catalog where entry_id in (select max(entry_id) from m_backup_catalog group by entry_type_name)"
  Disabled = false

[[Metrics]]
  Name = "hdb_cancelled_jobs"
  Help = "Sap jobs with status cancelled/aborted (today)"
  MetricType = "counter"
  TagFilter = ["abap"]
  SchemaFilter = ["sapabap1", "sapabap","sapewm"]
  SQL = "select count(*) from <SCHEMA>.tbtco where enddate=current_utcdate and status='A'"
  VersionFilter = ">= 2.00.040"
  Disabled = false

# Multi-metric query configuration example
[[Queries]]
  SQL = "SELECT operation_name, duration, error_code FROM <SCHEMA>.operations"
  TagFilter = ["abap"]
  SchemaFilter = ["sapabap1"]
  VersionFilter = ">= 2.00.040"
  Disabled = false

  [[Queries.Metrics]]
    Name = "hdb_operation_duration"
    Help = "Operation duration in milliseconds"
    MetricType = "gauge"
    ValueColumn = "duration"
    Unit = "ms"
    Labels = ["operation"]
    Disabled = false

  [[Queries.Metrics]]
    Name = "hdb_operation_errors"
    Help = "Number of failed operations"
    MetricType = "counter"
    ValueColumn = "error_code"
    Unit = ""
    Labels = ["operation"]
    Disabled = false
```

Below is a description of the tenant and metric struct fields:

#### Tenant information

| Field      | Type         | Description | Example |
| ---------- | ------------ |------------ | ------- |
| Name       | string       | SAP Hana tenant name | "P01", "q02" |
| Tags       | string array | Tags describing the system | ["abap", "erp"], ["systemdb"], ["java"] |
| ConnStr | string       | Connection string \<hostname\>:\<tenant sql port\> - the sql port can be selected in the following way on the system db: "select database_name,sql_port from sys_databases.m_services"  | "host.domain:31041" | 
| User       | string       | Tenant database user name | |
| Usage      | string       | Additional information about tenant usage | "Production", "Test" |
| Schemas    | string array | Available schemas for the tenant | ["SAPABAP1", "SAPHANADB"] |
| SID        | string       | SAP System ID | "PRD", "DEV" |
| InstanceNumber | string   | SAP instance number | "00", "01" |
| DatabaseName | string     | Database name | "HDB", "SYSTEMDB" |
| Version    | string       | Database version | "2.00.040" |
| Index      | int          | Tenant index in configuration | 0, 1, 2 |

#### Metric information

| Field        | Type         | Description | Example |
| ------------ | ------------ |------------ | ------- |
| Name         | string       | Metric name (words separated by underscore, otherwise a panic can occur)| "hdb_info" |
| Help         | string       | Metric help text | "Hana database version and uptime"|
| MetricType   | string       | Type of metric | "counter" or "gauge" |
| TagFilter    | string array | The metric will only be executed, if all values correspond with the existing tenant tags | TagFilter ["abap", "erp"] needs at least tenant Tags ["abap", "erp"] otherwise the metric will not be used |
| SchemaFilter | string array | The metric will only be used, if the tenant user has one of schemas in SchemaFilter assigned. The first matching schema will be replaced with the <SCHEMA> placeholder of the select.  | ["sapabap1", "sapewm"] |
| SQL          | string       | The select is responsible for the data retrieval. Conventionally the first column must represent the value of the metric. The following columns are used as labels and must be string values. The tenant name and the tenant usage are default labels for every metric and need not to be added in the select. | "select days_between(start_time, current_timestamp) as uptime, version from \<SCHEMA\>.m_database" (SCHEMA uppercase) |
| VersionFilter | string | Version filter (supports format: ">= 2.00.048"), execute this metric only when the tenant database version meets the condition | ">= 2.00.048" |
| ValueColumn   | string | Specifies the column name in the result set used for the metric value (used when SQL returns multiple numerical columns) | "uptime" |
| Unit          | string | Unit of measurement for the metric | "ms", "bytes" |
| Disabled      | bool   | When set to true, disables collection of this metric | false |

#### Query Information

| Field        | Type         | Description | Example |
| ------------ | ------------ |------------ | ------- |
| SQL          | string       | SQL query to execute | "SELECT operation_name, duration FROM operations" |
| TagFilter    | string array | The query will only be executed if all values correspond with the existing tenant tags | ["abap", "erp"] |
| SchemaFilter | string array | The query will only be used if the tenant user has one of schemas in SchemaFilter assigned | ["sapabap1", "sapewm"] |
| Metrics      | QueryMetricInfo array | Array of metrics to generate from this query | See QueryMetricInfo table |
| VersionFilter | string | Version filter (supports format: ">= 2.00.048") | ">= 2.00.048" |
| Disabled     | bool   | When set to true, disables this query | false |

#### Query Metric Information

| Field       | Type         | Description | Example |
| ----------- | ------------ |------------ | ------- |
| Name        | string       | Metric name | "hdb_operation_duration" | 
| Help        | string       | Metric help text | "Operation duration in milliseconds" |
| MetricType  | string       | Type of metric | "counter" or "gauge" |
| ValueColumn | string       | Column name in result set used for metric value | "duration" |
| Unit        | string       | Unit of measurement | "ms", "bytes" |
| Labels      | string array | Column names to use as labels | ["operation"] |
| Disabled    | bool         | When set to true, disables this metric | false |

#### Database passwords

With the following commands the passwords for the example tenants above can be written to the Secret section of the configfile:
```
$ ./hana_sql_exporter pw --tenant q01 --config ./hana_sql_exporter.toml
$ ./hana_sql_exporter pw -t qj1 -c ./.hana_sql_exporter.toml
```
With one password for multiple tenants, the following notation is also possible:
```
$ ./hana_sql_exporter pw --tenant q01,qj1 --config ./hana_sql_exporter.toml
```

## Usage

Now the web server can be started:
#### Binary

The default port is 9888 which can be changed with the -port flag. The standard timeout is set to 10 seconds, which means that if a scrape for one metric and tenant takes more than 10 seconds, it will be aborted. This is normally only the case, if a tenant is overloaded or the selects are really extensive. In my experience the scrapes for 25 tenants and 30 metrics in one config file take approximately 250ms altogether, if all tenants are responsive. Normally I set the timeout flag to 5 seconds, the scrape timeout for the corresponding Prometheus job to 10 seconds and the scrape intervall to one minute.

```
$ ./hana_sql_exporter web --config ./hana_sql_exporter.toml --timeout 5
```
Then you should be able to find the desired metrics after calling ``localhost:9888/metrics`` in the browser.

#### Docker
The Docker image can be downloaded from Docker Hub or built with the Dockerfile. Then it can be started as follows:
```
$ docker run -d --name=hana_exporter --restart=always -p 9888:9888 -v /home/<user>/.hana_sql_exporter.toml:/app/.hana_sql_exporter.toml <image name>
```
#### Kubernetes
An example config can be found in the examples folder. First of all create a sap namespace. Then apply the created configfile as configmap and start the deployment:
```
$ kubectl apply -f sap-namespace.yaml 
$ kubectl create configmap hana-config -n sap --from-file ./hana_sql_exporter.toml -o yaml
$ kubectl apply -f hana-deployment.yaml
```

Configfile changes can be applied in the following way:
```
$ kubectl create configmap hana-config -n sap --from-file ./hana_sql_exporter.toml -o yaml --dry-run | sudo kubectl replace -f -
$ kubectl scale --replicas=0 -n sap deployment hana-sql-exporter
$ kubectl scale --replicas=1 -n sap deployment hana-sql-exporter
```
#### Prometheus configfile
The necessary entries in the prometheus configfile can look something like the following:
```
  - job_name: sap
        scrape_interval: 60s
        static_configs:
          - targets: ['172.45.111.105:9888']
            labels:  {'instance': 'hana-exporter-test'}
          - targets: ['hana-exporter.sap.svc.cluster.local:9888']
            labels:  {'instance': 'hana-exporter-dev'}
```

## Result
The resulting information can be found in the Prometheus expression browser and can be used as normal for creating alerts or displaying dashboards in Grafana.

The image below shows for example the duration of all complete data backups. With one dashboard it is possible to detect hanging or aborted backups of all systems:

 ![backups](/examples/images/backups.png)

## More Information
* [Monitoring SAP and Hana Instances with Prometheus and Grafana](https://blogs.sap.com/2020/02/07/monitoring-sap-and-hana-instances-with-prometheus-and-grafana/)
