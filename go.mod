module github.com/notnotquinn/wts

go 1.18

require (
	github.com/notnotquinn/go-websub v0.2.1-0.20220401210256-463b0ef0c0e0
	go.kuoruan.net/v8go-polyfills v0.5.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	rogchap.com/v8go v0.7.0
)

require (
	github.com/google/uuid v1.3.0 // indirect
	github.com/rs/zerolog v1.26.1 // indirect
	github.com/tomnomnom/linkheader v0.0.0-20180905144013-02ca5825eb80 // indirect
	golang.org/x/net v0.0.0-20220401154927-543a649e0bdd // indirect
)

// Made PR to add these features, however it has not been merged yet and I would like to use them.
replace go.kuoruan.net/v8go-polyfills v0.5.0 => github.com/NotNotQuinn/v8go-polyfills v0.5.1-0.20220402045617-4e94d8bb5eed
