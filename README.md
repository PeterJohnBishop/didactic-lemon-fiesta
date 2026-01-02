# didactic-lemon-fiesta

A TCP Relay that uses secret-based matchmacking to connect two clients and relay data between them. 

- files now go to /Users/{username}/Downloads on MacOS

<!-- launch relay server -->
ROLE=server PORT=8080 go run main.go

<!-- launch test client A -->
ROLE=client SECRET=testContainerRelay go run main.go

<!-- launch test client B -->
ROLE=client SERVER_URL=localhost:8080 SECRET=todaysSecret2 FILE=/Users/peterbishop/Development/notes.md go run main.go

# idea

Client A (Primary) launches and reads a directory for all files. 

All files are chunked. 

Client B (Secondary) launches and reads a directory for all files. 

All files are chunked. 

Client A and Client B exchange manifests.

If a manifest for file X on Client A doesn't pass SHA256 hash verification for the same file on Client B, 

OR

if Client A has a file that Client B doesn't have, send the manifest from Client A to Client B, for Client B to download. 

### File Sync Example: Recieving a file placed into the corresponding directory on a separate device
![screenshot](https://github.com/PeterJohnBishop/TCP-Relay-ServerClient/blob/main/assets/Screenshot%202025-12-29%20at%202.28.14%E2%80%AFAM.png?raw=true)




