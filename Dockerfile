# Start by building the application.
FROM golang:1.13-buster as build

WORKDIR /code
ADD . /code

RUN go get -d -v ./...

RUN CGO_ENABLED=0 go build -o /code/app

# Now copy it into our base image.
FROM gcr.io/distroless/base-debian10
COPY --from=build /code/app /
CMD ["/app"]
