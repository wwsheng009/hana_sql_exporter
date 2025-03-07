
## SAP Hana SQL Exporter for Prometheus  [![CircleCI](https://circleci.com/gh/ulranh/hana_sql_exporter/tree/master.svg?style=svg)](https://circleci.com/gh/ulranh/hana_sql_exporter) [![Go Report Card](https://goreportcard.com/badge/github.com/ulranh/hana_sql_exporter)](https://goreportcard.com/report/github.com/ulranh/hana_sql_exporter)  [![Docker Pulls](https://img.shields.io/docker/pulls/ulranh/hana-sql-exporter)](https://hub.docker.com/r/ulranh/hana-sql-exporter)

该项目旨在通过 [Prometheus](https://prometheus.io) 和 [Grafana](https://grafana.com) 来支持对 SAP 和 SAP HanaDB 实例的监控。

## 安装

您可以下载[已发布版本](https://github.com/ulranh/hana_sql_exporter/releases/latest)。如果要构建当前版本，您需要安装 [Go](https://golang.org/) 编程语言：

```
$ git clone git@github.com:ulranh/hana_sql_exporter.git
$ cd hana_sql_exporter
$ go build
```

## 准备工作

#### 数据库用户
需要为每个租户创建一个具有所有相关 schema 读取权限的数据库用户：

```
# 使用具有授权的用户登录：
$ create user <user> password <pw> no force_first_password_change;
$ alter user <user> disable password lifetime;
$ grant catalog read to <user>;

# 使用具有授权的用户登录：
$ grant select on schema <schema> to <user>;
# <schema>: _SYS_STATISTICS, SAPABAP1, SAPHANADB ... 
```

#### 配置文件
下一个必要的部分是 [toml](https://github.com/toml-lang/toml) 配置文件，用于存储加密的密码、租户信息和指标信息。默认文件名为 .hana_sql_exporter.toml，默认位置在用户的主目录下。可以使用 --config (-c) 标志来指定其他位置或名称。

该文件包含一个 Tenants（租户）切片，后跟一个 Metrics（指标）切片：

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
  SchemaFilter = [] # sys schema 将被自动添加
  SQL = "select (case when state_name = 'successful' then 0 when state_name = 'running' then 1 else -1 end) as val, entry_type_name as type from <SCHEMA>.m_backup_catalog where entry_id in (select max(entry_id) from m_backup_catalog group by entry_type_name)"

[[Metrics]]
  Name = "hdb_cancelled_jobs"
  Help = "Sap jobs with status cancelled/aborted (today)"
  MetricType = "counter"
  TagFilter = ["abap"]
  SchemaFilter = ["sapabap1", "sapabap","sapewm"]
  SQL = "select count(*) from <SCHEMA>.tbtco where enddate=current_utcdate and status='A'"
```

以下是租户和指标结构字段的说明：

#### 租户信息

| 字段       | 类型         | 说明 | 示例 |
| ---------- | ------------ |------------ | ------- |
| Name       | string       | SAP Hana 租户名称 | "P01", "q02" |
| Tags       | string array | 描述系统的标签 | ["abap", "erp"], ["systemdb"], ["java"] |
| ConnStr    | string       | 连接字符串 \<hostname\>:\<tenant sql port\> - SQL 端口可以在系统数据库中通过以下方式查询："select database_name,sql_port from sys_databases.m_services" | "host.domain:31041" |
| User       | string       | 租户数据库用户名 | |

#### 指标信息

| 字段         | 类型         | 说明 | 示例 |
| ------------ | ------------ |------------ | ------- |
| Name         | string       | 指标名称（单词间用下划线分隔，否则可能会发生错误）| "hdb_info" |
| Help         | string       | 指标帮助文本 | "Hana database version and uptime"|
| MetricType   | string       | 指标类型 | "counter" 或 "gauge" |
| TagFilter    | string array | 仅当所有值与现有租户标签相对应时，才会执行该指标 | TagFilter ["abap", "erp"] 需要租户至少有 Tags ["abap", "erp"]，否则该指标不会被使用 |
| SchemaFilter | string array | 仅当租户用户具有 SchemaFilter 中的某个 schema 的权限时，才会使用该指标。第一个匹配的 schema 将替换 select 语句中的 <SCHEMA> 占位符 | ["sapabap1", "sapewm"] |
| SQL          | string       | 该 select 语句负责数据检索。按照惯例，第一列必须表示指标的值。后续列用作标签，必须是字符串值。租户名称和租户用途是每个指标的默认标签，无需在 select 语句中添加 | "select days_between(start_time, current_timestamp) as uptime, version from \<SCHEMA\>.m_database" (SCHEMA 大写) |

#### 数据库密码

使用以下命令可以将上述示例租户的密码写入配置文件的 Secret 部分：
```
$ ./hana_sql_exporter pw --tenant q01 --config ./hana_sql_exporter.toml
$ ./hana_sql_exporter pw -t qj1 -c ./.hana_sql_exporter.toml
```
对于多个租户使用相同密码的情况，也可以使用以下方式：
```
$ ./hana_sql_exporter pw --tenant q01,qj1 --config ./hana_sql_exporter.toml
```

## 使用方法

现在可以启动 Web 服务器：
#### 二进制文件

默认端口为 9658，可以通过 -port 标志更改。标准超时设置为 10 秒，这意味着如果一个指标和租户的抓取时间超过 10 秒，它将被中止。这种情况通常只发生在租户过载或 select 语句非常复杂时。根据经验，如果所有租户都响应正常，一个配置文件中 25 个租户和 30 个指标的抓取总共大约需要 250ms。通常我会将超时标志设置为 5 秒，相应的 Prometheus 作业的抓取超时设置为 10 秒，抓取间隔设置为一分钟。

```
$ ./hana_sql_exporter web --config ./hana_sql_exporter.toml --timeout 5
```
然后，您应该可以在浏览器中访问 `localhost:9658/metrics` 来查看所需的指标。

#### Docker
Docker 镜像可以从 Docker Hub 下载或使用 Dockerfile 构建。然后可以按以下方式启动：
```
$ docker run -d --name=hana_exporter --restart=always -p 9658:9658 -v /home/<user>/.hana_sql_exporter.toml:/app/.hana_sql_exporter.toml <image name>
```

#### Kubernetes
示例配置可以在 examples 文件夹中找到。首先创建一个 sap 命名空间。然后将创建的配置文件应用为 configmap 并启动部署：
```
$ kubectl apply -f sap-namespace.yaml 
$ kubectl create configmap hana-config -n sap --from-file ./hana_sql_exporter.toml -o yaml
$ kubectl apply -f hana-deployment.yaml
```

配置文件更改可以通过以下方式应用：
```
$ kubectl create configmap hana-config -n sap --from-file ./hana_sql_exporter.toml -o yaml --dry-run | sudo kubectl replace -f -
$ kubectl scale --replicas=0 -n sap deployment hana-sql-exporter
$ kubectl scale --replicas=1 -n sap deployment hana-sql-exporter
```

#### Prometheus 配置文件
Prometheus 配置文件中的必要条目可能如下所示：
```
  - job_name: sap
        scrape_interval: 60s
        static_configs:
          - targets: ['172.45.111.105:9658']
            labels:  {'instance': 'hana-exporter-test'}
          - targets: ['hana-exporter.sap.svc.cluster.local:9658']
            labels:  {'instance': 'hana-exporter-dev'}
```

## 结果
生成的信息可以在 Prometheus 表达式浏览器中找到，可以用于创建告警或在 Grafana 中显示仪表板。

下图显示了所有完整数据备份的持续时间。通过一个仪表板，可以检测到所有系统的挂起或中止的备份：

 ![backups](/examples/images/backups.png)

## change log
2020/10/13

add log file location config

"--logfile","/tmp/hana_sql_exporter.log"

add new database sql data type conversion support. as big.Rat => string 

## notice
the first sql first column must be numeric

## 更多信息
* [使用 Prometheus 和 Grafana 监控 SAP 和 Hana 实例](https://blogs.sap.com/2020/02/07/monitoring-sap-and-hana-instances-with-prometheus-and-grafana/)