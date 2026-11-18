module github.com/RyanHirsch/CLIProxyAPI/fork-plugins/clickhouse-log

go 1.26.10

require (
	github.com/MoeForge/moego v0.1.0-alpha.4
	github.com/router-for-me/CLIProxyAPI/v8 v8.0.0
	github.com/sirupsen/logrus v1.9.3
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/sys v0.48.0 // indirect

replace github.com/router-for-me/CLIProxyAPI/v8 => ../..
