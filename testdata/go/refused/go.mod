module example.com/refused

go 1.27.4

require github.com/CheckPickerUpper/nocontract v0.0.0

require (
	github.com/CheckPickerUpper/ErGo v0.0.0-20261118210535-5e49b5eba7bc // indirect
	github.com/CheckPickerUpper/outcome v0.0.0-20261116220618-27755f6ba168 // indirect
	github.com/creachadair/atomicfile v0.4.2 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/go-json-experiment/json v1.27.2 // indirect
	github.com/samber/mo v1.17.0 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/tools v0.53.0 // indirect
)

replace github.com/CheckPickerUpper/nocontract => ../../..

tool github.com/CheckPickerUpper/nocontract/cmd/nocontract
