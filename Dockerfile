FROM golang:1.13.6

WORKDIR /go/src/RossyWhite/konmari
COPY . .
RUN go build

FROM debian:stretch-slim

COPY --from=0 /go/src/RossyWhite/konmari/konmari/ /bin/konmari

CMD ["/bin/konmari"]