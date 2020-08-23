package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	flag.Bool("f", false, "continuous reading")
	flag.String("filter", "", "filter on (tag=value)")
	flag.String("cfg", ".jlv", "configuration file name (without extension)")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	viper.SetConfigName(viper.GetString("cfg"))
	viper.AddConfigPath("./cfg/")
	viper.AddConfigPath("./")
	viper.AutomaticEnv()
	viper.ReadInConfig()

	if pflag.NArg() == 0 {
		fmt.Println("no filename found")
		return
	}
	file, err := os.Open(pflag.Arg(0))
	if err != nil {
		fmt.Printf("error open file: %v\n", err)
	}
	f, err := NewFile(file)
	if err != nil {
		fmt.Printf("error reading file: %v\n", err)
	}
	err = startTerm(f.View())
	if err != nil {
		for i := 0; i < f.LinesCount(); i++ {
			fmt.Printf("%02d: %s\n", i, string(f.bytes(i)))
		}
	}

	// start(pflag.Arg(0))
}

func start(name string) {
	file, err := os.Open(name)
	if err != nil {
		fmt.Printf("error open file: %v\n", err)
	}
	s := bufio.NewScanner(file)
	f := viper.GetString("filter")
	ft := ""
	fv := ""
	if f != "" {
		parts := strings.Split(f, "=")
		if len(parts) != 2 {
			fmt.Println("invalid filter format")
			return
		}
		ft = strings.Trim(parts[0], " \t")
		fv = strings.Trim(parts[1], " \t")
	}
	for s.Scan() {
		m := map[string]interface{}{}
		err = json.Unmarshal(s.Bytes(), &m)
		if err != nil {
			fmt.Printf("problem while unmarshalling the line: %v\n", err)
			return
		}
		if f == "" || m[ft] == fv {
			fmt.Printf("%s: %s: %s\n", m["time"], m["level"], m["msg"])
		}
	}
}
