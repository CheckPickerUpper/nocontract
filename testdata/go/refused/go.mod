module example.com/refused

go 1.26.5

require github.com/CheckPickerUpper/nocontract v0.0.0

require (
	github.com/CheckPickerUpper/ErGo v0.0.0-20261115191336-da68dcb0f844 // indirect
	github.com/creachadair/atomicfile v0.4.2 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/go-json-experiment/json v1.27.2 // indirect
	github.com/samber/mo v1.17.0 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)

replace github.com/CheckPickerUpper/nocontract => ../../..

tool github.com/CheckPickerUpper/nocontract/cmd/nocontract
