resetkind:
	kind delete cluster && kind create cluster && flux install

run-example:
	go run . --envfile "./example/config.env" --knownNode kind-control-plane ./example

