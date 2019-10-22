GOARCH=amd64 GOOS=linux go build -o bin/savetopocketbot savetopocketbot.go
chmod +x bin/savetopocketbot
serverless deploy