package bind9Config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"BCDns_0.1/blockChain"
)

const (
	BindConfigFile = "./db.root"
	NameComment    = ";; "
)

var (
	Generator *ConfigGenerator
)

type ConfigGenerator struct{}

func init() {
	Generator = new(ConfigGenerator)
}

func (g *ConfigGenerator) Run() {
	for {
		select {
		//case <-time.After(conf.BCDnsConfig.ConfigInterval):
		case <-blockChain.NewBlockChan:
			generateConfig()
		}
	}
}

func generateConfig() {
	//f, err := os.OpenFile(BindConfigFile, os.O_WRONLY|os.O_TRUNC, 0644)
	f, err := os.OpenFile(BindConfigFile, os.O_RDWR, 0644)
	if err != nil {
		fmt.Printf("[generateConfig] error=%v\n", err)
		return
	}
	defer f.Close()
	config, err := blockChain.ZoneStatePool.GetModifiedData()
	if err != nil {
		fmt.Printf("[generateConfig] error=%v\n", err)
		return
	}
	if len(config) == 0 {
		return
	}

	reader, output, replace := bufio.NewReader(f), make([]byte, 0), false
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			fmt.Printf("[generateConfig] error=%v\n", err)
			break
		}
		if bytes.HasPrefix(line, []byte(NameComment)) {
			infos := bytes.Split(line, []byte(" "))
			zoneName := string(infos[1][:len(infos[1]])
			if zoneName == "over" {
				break
			}
			line = append(line, []byte("\n")...)
			output = append(output, line...)

			//if rrs, ok := config[string(infos[1])]; ok && rrs.Owner != messages.Dereliction && !replace {
			if rrs, ok := config[zoneName]; ok && !replace {
				replace = true
				output = append(output, generate(rrs)...)
				delete(config, zoneName)
			} else if replace {
				replace = false
			}
		} else if !replace {
			line = append(line, []byte("\n")...)
			output = append(output, line...)
		}
	}
	for zoneName, rrs := range config {
		output = append(output, []byte(NameComment+zoneName+"\n")...)
		output = append(output, generate(rrs)...)
	}
	line := ";; over\n"
	output = append(output, []byte(line)...)
	f.Truncate(0)
	f.Seek(0, 0)
	f.Write(output)
	cmd := exec.Command("rndc", "reload")
	if err := cmd.Run(); err != nil {
		fmt.Printf("[generateConfig] error=%v\n", err)
	}

}

func generate(rrs blockChain.ZoneRecord) []byte {
	output := make([]byte, 0)
	//output = append(output, []byte(NameComment+name+"\n")...)
	for _, rr := range rrs.Values {
		output = append(output, []byte(rr+"\n")...)
	}
	//output = append(output, []byte(NameComment+name+"\n")...)
	return output
}
