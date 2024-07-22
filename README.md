# go-cjs-module-lexer

## Installation

```sh
go get -u github.com/jcbhmr/go-cjs-module-lexer
```

## Usage

### Disable WebAssembly

If you prefer not to use WebAssembly you can use the `cjsmodulelexer.nowasm` build tag. This will use the pure Go implementation of the lexer.

```sh
go build -tags cjsmodulelexer.nowasm ./cmd/my-app
```

## Development

https://github.com/nodejs/cjs-module-lexer
