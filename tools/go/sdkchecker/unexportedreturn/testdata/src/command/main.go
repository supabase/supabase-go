package main

type handle struct{}

func Open() *handle { return &handle{} }

func main() { _ = Open() }
