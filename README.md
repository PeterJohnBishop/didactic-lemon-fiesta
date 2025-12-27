# didactic-lemon-fiesta

A basic relay server that uses binary protocol data transfer over TCP between multiple clients.

<!-- launch relay server -->
ROLE=server SERVER_PORT=:8080 go run main.go

<!-- launch test client A -->
ROLE=client SERVER_URL=localhost:8080 SECRET=todaysSecret go run main.go

<!-- launch test client B -->
ROLE=client SERVER_URL=localhost:8080 SECRET=todaysSecret FILE=/Users/peterbishop/Development/notes.md go run main.go