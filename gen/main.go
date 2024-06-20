package main

import "os"

func main() {
	if err := os.Chdir(".."); err != nil {
		panic(err)
	}
	avx512()
	if err := slide(); err != nil {
		panic(err)
	}
}
