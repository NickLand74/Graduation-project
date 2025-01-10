    FROM golang:1.22.6 AS builder

    WORKDIR /app

    COPY go.mod go.sum ./

    RUN go mod download

    COPY . .

    ENV CGO_ENABLED=0

    ENV GOOS=linux

    RUN go build -o main main.go

    FROM scratch
    
    WORKDIR /app
    
    COPY --from=builder /app/main /app/
    
    COPY --from=builder /app/web /app/web
    
    EXPOSE ${APP_PORT:-7540}
    
    CMD ["/app/main"]