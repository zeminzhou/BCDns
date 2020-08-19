package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

type OperationType uint8

const (
	Add OperationType = iota
	Del
	Mod
)

type Order struct {
	OptType  OperationType
	ZoneName string
	Values   []string
}

func main() {
	rUdpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8888")
	if err != nil {
		panic(err)
	}

	lUdpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8887")
	if err != nil {
		panic(err)
	}

	conn, err := net.DialUDP("udp", lUdpAddr, rUdpAddr)
	if err != nil {
		panic(err)
	}

	input := bufio.NewScanner(os.Stdin)

	for {
		fmt.Printf(">")
		input.Scan()

		cmd := input.Text()
		if cmd == "quit" {
			fmt.Println("bye")
			break
		}

		if (cmd == "help") || (cmd != "add" && cmd != "del" && cmd != "mod") {
			fmt.Println("  command \"add\" for adding RRs")
			fmt.Println("  command \"mod\" for modifying RRs")
			fmt.Println("  command \"del\" for deleting RRs")
			fmt.Println("  input \"quit\" quit cli")
			continue
		}

		fmt.Printf("Please input ZoneName >")
		input.Scan()
		zoneName := input.Text()
		zoneName = strings.Replace(zoneName, " ", "", -1)

		fmt.Printf("Please input RRs with \"over\" ending input \n>")
		var values []string
		for input.Scan() {
			rr := input.Text()
			if rr == "over" {
				break
			}

			values = append(values, rr)
			fmt.Printf("RRs >")
		}

		if len(values) == 0 {
			continue
		}

		msg := Order{
			OptType:  Add,
			ZoneName: zoneName,
			Values:   values,
		}

		if cmd == "mod" {
			msg.OptType = Mod
		}

		if cmd == "del" {
			msg.OptType = Del
		}

		jsonData, err := json.Marshal(msg)
		if err != nil {
			panic(err)
		}
		conn.Write(jsonData)

		fmt.Printf("finish commad %s\n", cmd)
	}
}
