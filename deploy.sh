GOARCH=amd64 GOOS=linux go build -o bin/savetopocketbot main/savetopocketbot.go
chmod +x bin/savetopocketbot
serverless deploy