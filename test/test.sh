
#!/bin/bash


# set password secret
../main pw --tenant HDB --config ./hana_sql_exporter.toml

# test the web server
../main web --config ./hana_sql_exporter.toml
