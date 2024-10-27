FROM golang:1.23

WORKDIR /app

RUN git clone https://github.com/shazow/ssh-chat.git && \
    cd ssh-chat && \
    go build ./cmd/ssh-chat

CMD ["./ssh-chat/ssh-chat"]
