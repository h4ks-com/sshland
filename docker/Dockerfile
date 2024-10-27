FROM golang:1.23
WORKDIR /app
COPY . .
RUN go mod download

# Create user without shell login

CMD ["go", "run", "main.go"]
