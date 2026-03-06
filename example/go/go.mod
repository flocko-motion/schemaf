module schemaf.local/example

go 1.25.0

require (
	github.com/google/uuid v1.6.0
	schemaf.local/base v0.0.0
)

require github.com/lib/pq v1.11.2 // indirect

replace schemaf.local/base => ../../go
