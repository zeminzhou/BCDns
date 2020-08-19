package main

import (
	"C"
	"fmt"
	"time"

	"BCDns_0.1/bcDns/conf"
	blockChain2 "BCDns_0.1/blockChain"
	service2 "BCDns_0.1/certificateAuthority/service"
	mybft "BCDns_0.1/consensus/consensusMy/service"
	pbft "BCDns_0.1/consensus/consensusPBFT/service"
	"BCDns_0.1/consensus/model"
	dao2 "BCDns_0.1/dao"
	service3 "BCDns_0.1/network/service"
	"BCDns_0.1/utils"
)

var (
	ConsensusCenter model.ConsensusI
)

func main() {
	Start()
}

//export Start()
func Start() {

	//go func() {
	//	http.HandleFunc("/debug/pprof/block", pprof.Index)
	//	http.HandleFunc("/debug/pprof/goroutine", pprof.Index)
	//	http.HandleFunc("/debug/pprof/heap", pprof.Index)
	//	http.HandleFunc("/debug/pprof/threadcreate", pprof.Index)
	//
	//	http.ListenAndServe("0.0.0.0:9999", nil)
	//}()
	initLeaderDone := make(chan uint)
	done := make(chan uint)
	var err error
	fmt.Println("[Init Dao]")
	dao2.Dao, err = NewDao()
	if err != nil {
		panic(err)
	}
	defer blockChain2.BlockChain.Close()
	fmt.Println("[Init NetWork]")
	service3.Net, err = service3.NewDNet()
	if err != nil {
		panic(err)
	}
	if service3.Net == nil {
		panic("NewDNet failed")
	}
	fmt.Println("[Init consensus]")
	switch conf.BCDnsConfig.Mode {
	case "MYBFT":
		ConsensusCenter, err = mybft.NewConsensus()
	case "PBFT":
		ConsensusCenter, err = pbft.NewConsensus()
	}
	if err != nil {
		panic(err)
	}
	fmt.Println("[start init leader]")
	go ConsensusCenter.Start(initLeaderDone)
	fmt.Println("[Join]")
	err = service3.Net.Join(service2.CertificateAuthorityX509.GetSeeds(), 0)
	if err != nil {
		panic(err)
	}
	_ = <-initLeaderDone
	if ConsensusCenter.IsLeader() {
		conf.BCDnsConfig.Byzantine = false
	}
	fmt.Println("[System running]")
	fmt.Println("[Start Time]", time.Now())
	utils.SendStatus(ConsensusCenter.IsLeader())
	//	go bind9Config.Generator.Run()
	go ConsensusCenter.Run(done)
	_ = <-done
	fmt.Println("[Err] System exit")
}

//export CheckRRs
func CheckRRs(rrsCount int, rrBytes []byte) int {
	rrs := []string{}
	rrName := ""
	rrType := ""
	rrValue := ""
	l := 0

	for i := 0; i < rrsCount; i++ {
		rrName, l = toString(rrBytes)
		rrBytes = rrBytes[l:]

		if rrBytes[0] == 2 {
			rrType = "NS"
		} else if rrBytes[0] == 1 {
			rrType = "A"
		} else if rrBytes[0] == 27 {
			rrType = "AAAA"
		} else {
			fmt.Println("[CheckRRs] Parse error")
			return 1
		}
		rrBytes = rrBytes[2:]

		rrValue, l = toString(rrBytes)
		rrBytes = rrBytes[l:]

		rr := rrName + " IN " + rrType + " " + rrValue
		fmt.Println("[CheckRR] rr: ", rr)
		rrs = append(rrs, rr)
	}

	bcc := blockChain2.GetBlockchainCache(blockChain2.BlockChain)
	if bcc.CheckRRs(rrs) {
		return 1
	} else {
		return 0
	}
}

func toString(bytes []byte) (string, int) {
	s := ""
	i := 0
	for ; i < len(bytes) && bytes[i] != 0; i++ {
		s += string(bytes[i])
	}
	return s, i + 1
}

func NewDao() (*dao2.DAO, error) {
	var err error
	blockChain2.BlockChain, err = blockChain2.NewBlockchain(conf.BCDnsConfig.HostName)
	if err != nil {
		return nil, err
	}
	dao2.Db, err = dao2.NewDB(conf.BCDnsConfig.HostName)
	if err != nil {
		return nil, err
	}
	storage := dao2.NewStorage(dao2.Db, blockChain2.BlockChain)
	return &dao2.DAO{
		Storage: storage,
	}, nil
}
