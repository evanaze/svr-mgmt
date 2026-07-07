{...}: {
  # https://devenv.sh/languages/
  languages.go.enable = true;

  # https://devenv.sh/scripts/
  scripts.build.exec = "go build -o svr-mgmt .";

  # https://devenv.sh/tests/
  enterTest = "CGO_ENABLED=0 go test ./...";
}
