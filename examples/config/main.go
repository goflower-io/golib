package main

import (
	"flag"
	"fmt"

	"github.com/goflower-io/golib/cfg"
)

type Demo struct {
	Name string
}

func main() {
	flag.Parse()
	cf := cfg.Config()
	fmt.Println(cf.RawString("oo.json"))
	fmt.Println(cf.RawString("oo.toml"))
	fmt.Println(cf.RawString("oo.yml"))
	if ch, err := cf.Watch("oo.json"); err != nil {
		panic(err)
	} else {
		for x := range ch {
			fmt.Println(string(x))
		}
	}
	x := &Demo{}
	cf.UnmarshalTo("oo.json", x)
	fmt.Println(x)
	cf.UnmarshalTo("oo.toml", x)
	fmt.Println(x)
	cf.UnmarshalTo("oo.yml", x)
	fmt.Println(x)

	m := make(chan int)
	<-m
}
