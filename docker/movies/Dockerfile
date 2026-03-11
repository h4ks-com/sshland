FROM golang:1.23-alpine AS go-builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /build/movies-bin ./movies/

FROM golang:1.24-alpine AS ascii-movie-builder

RUN CGO_ENABLED=0 go install gabe565.com/ascii-movie@latest
# sw1 (Star Wars) is embedded in the ascii-movie binary; no download needed.

FROM alpine:3.21 AS c-builder

RUN apk add --no-cache gcc musl-dev make git ncurses-dev pkgconf

RUN git clone --depth=1 https://github.com/klange/nyancat.git /nyancat && \
    cd /nyancat && make

COPY docker/movies/donut.c /donut.c
RUN gcc -O2 -o /donut /donut.c -lm

COPY docker/movies/fire.c /fire.c
RUN gcc -O2 -o /fire /fire.c

COPY docker/movies/gol.c /gol.c
RUN gcc -O2 -o /gol /gol.c

RUN git clone --depth=1 https://gitlab.com/jallbrit/cbonsai.git /cbonsai && \
    cd /cbonsai && make

FROM alpine:3.21

RUN apk add --no-cache ncurses-libs ncurses-terminfo-base && \
    addgroup -S sshland && \
    adduser -S -G sshland -s /sbin/nologin -H sshland && \
    mkdir -p /proxy_key && \
    chown sshland:sshland /proxy_key

COPY --from=go-builder /build/movies-bin /usr/local/bin/movies
COPY --from=ascii-movie-builder /go/bin/ascii-movie /usr/local/bin/ascii-movie
COPY --from=c-builder /nyancat/src/nyancat /usr/local/bin/nyancat
COPY --from=c-builder /donut /usr/local/bin/donut
COPY --from=c-builder /cbonsai/cbonsai /usr/local/bin/cbonsai
COPY --from=c-builder /fire /usr/local/bin/fire
COPY --from=c-builder /gol /usr/local/bin/gol

USER sshland

EXPOSE 2222 2223 2224 2225 2226 2227 2228 2229

ENTRYPOINT ["/usr/local/bin/movies"]
