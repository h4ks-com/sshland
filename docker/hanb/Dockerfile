FROM mattfly/hanb:main as builder
FROM golang:1.23
COPY --from=builder /hanb/hanb /bin/hanb

COPY . .
RUN go mod download
CMD ["go", "run", "main.go", "/bin/hanb"]
