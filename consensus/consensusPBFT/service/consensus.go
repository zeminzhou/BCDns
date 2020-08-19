package service

import (
	"BCDns_0.1/bcDns/conf"
	"BCDns_0.1/blockChain"
	service2 "BCDns_0.1/certificateAuthority/service"
	"BCDns_0.1/consensus/model"
	"BCDns_0.1/messages"
	"BCDns_0.1/network/service"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/op/go-logging"
	"net"
	"reflect"
	"sync"
	"time"
)

const (
	unreceived uint8 = iota
	keep
	drop
)

const (
	ok uint8 = iota
	dataSync
	invalid
)

var (
	blockChan  chan model.BlockMessage
	logger     *logging.Logger // package-level logger
	UdpAddress = "127.0.0.1:8888"
)

type ConsensusPBFT struct {
	Mutex sync.Mutex

	//Proposer role
	Proposals      map[string]messages.ProposalMessage
	proposalsTimer map[string]time.Time
	Replies        map[string]map[string]uint8
	Contexts       map[string]context.CancelFunc
	Conn           *net.UDPConn
	OrderChan      chan []byte
	PCount         uint

	//Node role
	ProposalsCache  map[string]uint8            //need clean used for start view change
	Blocks          []blockChain.BlockValidated //Block's hole
	BlockMessages   []model.BlockMessage        // need clean
	Block           map[string]model.BlockMessage
	BlockPrepareMsg map[string]map[string][]byte
	PrepareSent     map[string]bool
	BlockCommitMsg  map[string]map[string][]byte
	PPCount         uint
	PPPCount        uint

	//Leader role
	MessagePool  messages.ProposalMessagePool
	BlockConfirm bool
	UnConfirmedH uint
	PPPPCount    uint

	//View role
	OnChange           bool
	View               int64
	LeaderId           int64
	ViewChangeMsgs     map[string]model.ViewChangeMessage
	JoinReplyMessages  map[string]service.JoinReplyMessage
	JoinMessages       map[string]service.JoinMessage
	InitLeaderMessages map[string]service.InitLeaderMessage
}

type Order struct {
	OptType  messages.OperationType
	ZoneName string
	Values   []string
}

func init() {
	blockChan = make(chan model.BlockMessage, service.ChanSize)
	logger = logging.MustGetLogger("consensusMy")
}

func NewConsensus() (model.ConsensusI, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", UdpAddress)
	if err != nil {
		panic(err)
	}
	conn, err := net.ListenUDP("udp", udpaddr)
	if err != nil {
		panic(err)
	}
	consensus := &ConsensusPBFT{
		Mutex:          sync.Mutex{},
		Proposals:      map[string]messages.ProposalMessage{},
		proposalsTimer: map[string]time.Time{},
		Replies:        map[string]map[string]uint8{},
		Contexts:       map[string]context.CancelFunc{},
		Conn:           conn,
		OrderChan:      make(chan []byte, 1024),

		ProposalsCache:  map[string]uint8{},
		Blocks:          []blockChain.BlockValidated{},
		BlockMessages:   []model.BlockMessage{},
		Block:           map[string]model.BlockMessage{},
		BlockPrepareMsg: map[string]map[string][]byte{},
		PrepareSent:     map[string]bool{},
		BlockCommitMsg:  map[string]map[string][]byte{},

		MessagePool:  messages.NewProposalMessagePool(),
		BlockConfirm: true,
		UnConfirmedH: 0,

		OnChange:           false,
		View:               -1,
		LeaderId:           -1,
		ViewChangeMsgs:     map[string]model.ViewChangeMessage{},
		JoinMessages:       map[string]service.JoinMessage{},
		JoinReplyMessages:  map[string]service.JoinReplyMessage{},
		InitLeaderMessages: map[string]service.InitLeaderMessage{},
	}
	return consensus, nil
}

func (c *ConsensusPBFT) Start(done chan uint) {
	for {
		select {
		case msg := <-service.JoinReplyChan:
			if c.View != -1 {
				continue
			}
			if msg.View != -1 {
				c.View = msg.View
				c.LeaderId = c.View % int64(service2.CertificateAuthorityX509.GetNetworkSize())
				done <- 0
				continue
			}
			c.JoinReplyMessages[msg.From] = msg
			if service2.CertificateAuthorityX509.Check(len(c.JoinReplyMessages) + len(c.JoinMessages)) {
				initLeaderMsg, err := service.NewInitLeaderMessage(service.Net.GetAllNodeIds())
				if err != nil {
					logger.Warningf("[ViewManagerT.Start] NewInitLeaderMessage error=%v", err)
					panic(err)
				}
				jsonData, err := json.Marshal(initLeaderMsg)
				if err != nil {
					logger.Warningf("[ViewManagerT.Start] json.Marshal error=%v", err)
					panic(err)
				}
				service.Net.BroadCast(jsonData, service.InitLeaderMsg)
			}
		case msg := <-service.JoinChan:
			replyMsg, err := service.NewJoinReplyMessage(c.View, map[string][]byte{})
			if err != nil {
				logger.Warningf("[Network] handleConn NewJoinReplyMessage error=%v", err)
				continue
			}
			jsonData, err := json.Marshal(replyMsg)
			if err != nil {
				logger.Warningf("[Network] handleConn json.Marshal error=%v", err)
				continue
			}
			service.Net.SendTo(jsonData, service.JoinReplyMsg, msg.From)
			c.JoinMessages[msg.From] = msg
			if c.View == -1 && service2.CertificateAuthorityX509.Check(len(c.JoinReplyMessages)+len(c.JoinMessages)) {
				initLeaderMsg, err := service.NewInitLeaderMessage(service.Net.GetAllNodeIds())
				if err != nil {
					logger.Warningf("[ViewManagerT.Start] NewInitLeaderMessage error=%v", err)
					panic(err)
				}
				jsonData, err := json.Marshal(initLeaderMsg)
				if err != nil {
					logger.Warningf("[ViewManagerT.Start] json.Marshal error=%v", err)
					panic(err)
				}
				service.Net.BroadCast(jsonData, service.InitLeaderMsg)
			}
		case msgByte := <-service.InitLeaderChan:
			var msg service.InitLeaderMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[ViewManagerT.Start] json.Unmarshal error+%v", err)
				continue
			}
			if !msg.VerifySignature() {
				logger.Warningf("[ViewManagerT.Start] InitLeaderMeseaderId + 1)sage.VerifySignature failed")
				continue
			}
			c.InitLeaderMessages[msg.From] = msg
			if c.View == -1 && service2.CertificateAuthorityX509.Check(len(c.InitLeaderMessages)) {
				c.View, c.LeaderId = c.GetLeaderNode()
				if c.View == -1 {
					panic("[ViewManagerT.Start] GetLeaderNode failed")
				}
				done <- 0
				continue
			}
		}
	}
}

func (c *ConsensusPBFT) Run(done chan uint) {
	var (
		err error
	)
	defer close(done)
	go c.ReceiveOrder()
	interrupt := make(chan int)
	go func() {
		for {
			select {
			case <-time.After(10 * time.Second):
				fmt.Println("Timeout", c.BlockConfirm, c.UnConfirmedH)
				if c.BlockConfirm {
					interrupt <- 1
				}
			}
		}
	}()
	for {
		select {
		// Proposer role
		case msgByte := <-service.ProposalReplyChan:
			var msg messages.ProposalReplyMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Proposer.Run] json.Unmarshal error=%v", err)
				continue
			}
			if !msg.VerifySignature() {
				logger.Warningf("[Proposer.Run] Signature is invalid")
				continue
			}
			c.Mutex.Lock()
			if _, ok := c.Proposals[string(msg.Id)]; ok {
				c.Replies[string(msg.Id)][msg.From] = 0
				if service2.CertificateAuthorityX509.Check(len(c.Replies[string(msg.Id)])) {
					fmt.Printf("%v %v %v %v %v [Proposer.Run] ProposalMsgT execute successfully %v %v\n", time.Now().Unix(), c.PCount,
						c.PPCount, c.PPPCount, c.PPPPCount, c.Proposals[string(msg.Id)],
						time.Now().Sub(c.proposalsTimer[string(msg.Id)]).Seconds())
					delete(c.Proposals, string(msg.Id))
					delete(c.Replies, string(msg.Id))
					c.Contexts[string(msg.Id)]()
					delete(c.Contexts, string(msg.Id))
				}
			}
			c.Mutex.Unlock()
		case msgByte := <-c.OrderChan:
			var msg Order
			err = json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Proposer.Run] order json.Unmarshal error=%v", err)
				continue
			}
			c.handleOrder(msg)
		// Node role
		case msgByte := <-service.ProposalChan:
			var proposal messages.ProposalMessage
			err := json.Unmarshal(msgByte, &proposal)
			if err != nil {
				logger.Warningf("[Node.Run] json.Unmarshal error=%v", err)
				continue
			}
			c.PPPCount++
			if _, exist := c.ProposalsCache[string(proposal.Id)]; !exist {
				if !c.handleProposal(proposal) {
					continue
				}
				c.PPCount++
				c.ProposalsCache[string(proposal.Id)] = unreceived
				if c.IsLeader() {
					c.MessagePool.AddProposal(proposal)
					if c.BlockConfirm && c.MessagePool.Size() >= blockChain.BlockMaxSize {
						c.generateBlock()
					}
				}
				//} else {
				//	name, err := utils.GetCertId(*service2.CertificateAuthorityX509.CertificatesOrder[c.LeaderId])
				//	if err != nil {
				//		logger.Warningf("[SendToLeader] GetCertId failed err=%v", err)
				//		continue
				//	}
				//	service.Net.SendTo(msgByte, service.ProposalMsg, name)
				//}
			}
		case blockMsg := <-blockChan:
			c.ProcessBlockMessage(&blockMsg)
		case msgByte := <-service.BlockChan:
			if c.IsOnChanging() {
				//TODO Add feedback mechanism which send msg to client
				continue
			}
			var msg model.BlockMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Node.Run] json.Unmarshal error=%v", err)
				continue
			}
			c.ProcessBlockMessage(&msg)
		case msgByte := <-service.BlockConfirmChan:
			var msg messages.BlockConfirmMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Node.Run] json.Unmarshal error=%v", err)
				continue
			}
			if msg.View != c.View {
				continue
			}
			if !msg.VerifySignature() {
				logger.Warningf("[Node.Run] msg.VerifySignature failed")
				continue
			}
			if !msg.VerifyProof() {
				logger.Warningf("[Node.Run] msg.VerifyProof failed")
				continue
			}
			if _, ok := c.BlockPrepareMsg[string(msg.Id)]; !ok {
				c.BlockPrepareMsg[string(msg.Id)] = map[string][]byte{}
				c.PrepareSent[string(msg.Id)] = false
			}
			c.BlockPrepareMsg[string(msg.Id)][msg.From] = msg.Proof
			if _, ok := c.Block[string(msg.Id)]; ok && service2.CertificateAuthorityX509.Check(len(c.BlockPrepareMsg[string(msg.Id)])) &&
				!c.PrepareSent[string(msg.Id)] {
				blockCommitMsg, err := messages.NewBlockCommitMessage(c.View, msg.Id)
				if err != nil {
					logger.Warningf("[Node.Run] NewBlockCommitMessage error=%v", err)
					continue
				}
				jsonData, err := json.Marshal(blockCommitMsg)
				if err != nil {
					logger.Warningf("[Node.Run] json.Marshal error=%v", err)
					continue
				}
				c.PrepareSent[string(msg.Id)] = true
				service.Net.BroadCast(jsonData, service.BlockCommitMsg)
			}
		case msgByte := <-service.BlockCommitChan:
			var msg messages.BlockCommitMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Node.Run] json.Unmarshal error=%v", err)
				continue
			}
			if msg.View != c.View {
				continue
			}
			if !msg.VerifySignature() {
				logger.Warningf("[Node.Run] msg.VerifySignature failed")
				continue
			}
			if _, ok := c.BlockCommitMsg[string(msg.Id)]; !ok {
				c.BlockCommitMsg[string(msg.Id)] = map[string][]byte{}
			}
			c.BlockCommitMsg[string(msg.Id)][msg.From] = msg.Proof
			if _, ok := c.Block[string(msg.Id)]; ok && service2.CertificateAuthorityX509.Check(len(c.BlockCommitMsg[string(msg.Id)])) {
				blockValidated := blockChain.NewBlockValidated(c.Block[string(msg.Id)].Block, c.BlockCommitMsg[string(msg.Id)])
				if blockValidated == nil {
					logger.Warningf("[Node.Run] NewBlockValidated failed")
					continue
				}

				fmt.Println("Road", 1)
				c.ExecuteBlock(blockValidated)
				delete(c.BlockCommitMsg, string(msg.Id))
				delete(c.BlockPrepareMsg, string(msg.Id))
				delete(c.Block, string(msg.Id))
			}
		case msgByte := <-service.DataSyncChan:
			var msg blockChain.DataSyncMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Node.Run] json.Unmarshal error=%v", err)
				continue
			}
			if !msg.VerifySignature() {
				logger.Warningf("[Node.Run] DataSyncMessage.VerifySignature failed")
				continue
			}
			block, err := blockChain.BlockChain.GetBlockByHeight(msg.Height)
			if err != nil {
				logger.Warningf("[Node.Run] GetBlockByHeight error=%v", err)
				continue
			}
			respMsg, err := blockChain.NewDataSyncRespMessage(block)
			if err != nil {
				logger.Warningf("[Node.Run] NewDataSyncRespMessage error=%v", err)
				continue
			}
			jsonData, err := json.Marshal(respMsg)
			if err != nil {
				logger.Warningf("[Node.Run json.Marshal error=%v", err)
				continue
			}
			service.Net.SendTo(jsonData, service.DataSyncRespMsg, msg.From)
		case msgByte := <-service.DataSyncRespChan:
			var msg blockChain.DataSyncRespMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Node.Run] json.Unmarshal error=%v", err)
				continue
			}
			if !msg.Validate() {
				logger.Warningf("[Node.Run] DataSyncRespMessage.Validate failed")
				continue
			}
			fmt.Println("Road", 3)
			c.ExecuteBlock(&msg.BlockValidated)
		case msgByte := <-service.ProposalConfirmChan:
			var msg messages.ProposalConfirm
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[Node.Run] json.Unmarshal error=%v", err)
				continue
			}
			if state, ok := c.ProposalsCache[string(msg.ProposalHash)]; !ok || state == drop {
				logger.Warningf("[Node.Run] I have never received this proposal, exist=%v state=%v", ok, state)
				continue
			} else if state == unreceived {
				//TODO start view change
				c.StartViewChange()
			} else {
				//This proposal is unready
				logger.Warningf("[Node.Run] proposal is unready")
			}
		// Leader role
		case <-interrupt:
			c.generateBlock()

		// Viewchange
		case msgByte := <-service.ViewChangeChan:
			var msg model.ViewChangeMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[View.Run] json.Unmarshal error=%v", err)
				continue
			}
			if msg.View != c.View {
				continue
			}
			if !msg.VerifySignature() {
				continue
			}
			if !msg.VerifySignatures() {
				continue
			}
			c.ViewChangeMsgs[msg.From] = msg
			if service2.CertificateAuthorityX509.Check(len(c.ViewChangeMsgs)) {
				c.StartChange()
			}
		case msgByte := <-service.NewViewChan:
			var msg model.NewViewMessage
			err := json.Unmarshal(msgByte, &msg)
			if err != nil {
				logger.Warningf("[View.Run] json.Unmarshal error=%v", err)
				continue
			}
			if msg.View != c.View+1 {
				continue
			}
			if !msg.VerifySignature() {
				continue
			}
			c.ProcessNewViewMsg(&msg)
		}
	}
}

func (c *ConsensusPBFT) ReceiveOrder() {
	for true {
		data := make([]byte, 1024)
		len, err := c.Conn.Read(data)
		if err != nil {
			fmt.Printf("[Run] Proposer read order failed err=%v\n", err)
			continue
		}
		c.OrderChan <- data[:len]
	}
}

func (c *ConsensusPBFT) handleOrder(msg Order) {
	if proposal := messages.NewProposal(msg.ZoneName, msg.OptType, msg.Values); proposal != nil {
		proposalByte, err := json.Marshal(proposal)
		if err != nil {
			logger.Warningf("[handleOrder] json.Marshal error=%v", err)
			return
		}
		c.Mutex.Lock()
		c.Proposals[string(proposal.Id)] = *proposal
		c.proposalsTimer[string((proposal.Id))] = time.Now()
		c.Replies[string(proposal.Id)] = map[string]uint8{}
		ctx, cancelFunc := context.WithCancel(context.Background())
		go c.timer(ctx, proposal)
		c.Contexts[string(proposal.Id)] = cancelFunc
		c.Mutex.Unlock()
		c.PCount++
		service.Net.BroadCast(proposalByte, service.ProposalMsg)
	} else {
		logger.Warningf("[handleOrder] NewProposal failed")
	}
}

func (c *ConsensusPBFT) timer(ctx context.Context, proposal *messages.ProposalMessage) {
	select {
	case <-time.After(conf.BCDnsConfig.ProposalTimeout):
		c.Mutex.Lock()
		defer c.Mutex.Unlock()
		replies, ok := c.Replies[string(proposal.Id)]
		if !ok {
			return
		}
		if service2.CertificateAuthorityX509.Check(len(replies)) {
			fmt.Printf("%v %v %v %v %v [Proposer.Run] ProposalMsgT execute successfully %v %v\n", time.Now().Unix(),
				c.PCount, c.PPCount, c.PPPCount, c.PPPPCount, c.Proposals[string(proposal.Id)],
				time.Now().Sub(c.proposalsTimer[string(proposal.Id)]).Seconds())
			delete(c.Proposals, string(proposal.Id))
			delete(c.Replies, string(proposal.Id))
			delete(c.Contexts, string(proposal.Id))
		} else {
			confirmMsg := messages.NewProposalConfirm(proposal.Id)
			if confirmMsg == nil {
				logger.Warningf("[Proposer.timer] NewProposalConfirm failed")
				return
			}
			confirmMsgByte, err := json.Marshal(confirmMsg)
			if err != nil {
				logger.Warningf("[Proposer.timer] json.Marshal error=%v", err)
				return
			}
			service.Net.BroadCast(confirmMsgByte, service.ProposalConfirmMsg)
		}
	case <-ctx.Done():
	}
}

func (*ConsensusPBFT) handleProposal(proposal messages.ProposalMessage) bool {
	switch proposal.Type {
	case messages.Add:
		if !proposal.ValidateAdd() {
			logger.Warningf("[handleProposal] ValidateAdd failed")
			return false
		}
	case messages.Del:
		if !proposal.ValidateDel() {
			logger.Warningf("[handleProposal] ValidateDel failed")
			return false
		}
	case messages.Mod:
		if !proposal.ValidateMod() {
			logger.Warningf("[handleProposal] ValidateMod failed")
			return false
		}
	}
	return true
}

func (c *ConsensusPBFT) ValidateBlock(msg *model.BlockMessage) uint8 {
	if msg.View != c.View {
		logger.Warningf("[Node.Run] view is invalid")
		return invalid
	}
	lastBlock, err := blockChain.BlockChain.GetLatestBlock()
	if err != nil {
		logger.Warningf("[Node.Run] DataSync GetLatestBlock error=%v", err)
		return invalid
	}
	prevHash, err := lastBlock.Hash()
	if err != nil {
		logger.Warningf("[Node.Run] lastBlock.Hash error=%v", err)
		return invalid
	}
	if lastBlock.Height < msg.Block.Height-1 {
		StartDataSync(lastBlock.Height+1, msg.Block.Height-1)
		c.EnqueueBlockMessage(msg)
		return dataSync
	}
	if lastBlock.Height > msg.Block.Height-1 {
		logger.Warningf("[Node.Run] Block is out of time")
		return invalid
	}
	if bytes.Compare(msg.Block.PrevBlock, prevHash) != 0 {
		logger.Warningf("[Node.Run] PrevBlock is invalid")
		return invalid
	}
	if !msg.VerifyBlock() {
		logger.Warningf("[ValidateBlock] VerifyBlock failed")
		return invalid
	}
	if !msg.VerifySignature() {
		logger.Warningf("[ValidateBlock] VerifySignature failed")
		return invalid
	}
	if !ValidateProposals(msg) {
		logger.Warningf("[ValidateBlock] ValidateProposals failed")
		return invalid
	}
	return ok
}

func (c *ConsensusPBFT) EnqueueBlockMessage(msg *model.BlockMessage) {
	insert := false
	for i, b := range c.BlockMessages {
		if msg.Height < b.Height {
			c.BlockMessages = append(c.BlockMessages[:i+1], c.BlockMessages[i:]...)
			c.BlockMessages[i] = *msg
			insert = true
			break
		} else if msg.Height == b.Height {
			insert = true
			break
		}
	}
	if !insert {
		c.BlockMessages = append(c.BlockMessages, *msg)
	}
}

func (c *ConsensusPBFT) EnqueueBlock(block blockChain.BlockValidated) {
	insert := false
	for i, b := range c.Blocks {
		if block.Height < b.Height {
			c.Blocks = append(c.Blocks[:i+1], c.Blocks[i:]...)
			c.Blocks[i] = block
			insert = true
			break
		} else if block.Height == b.Height {
			insert = true
			break
		}
	}
	if !insert {
		c.Blocks = append(c.Blocks, block)
	}
}

func (c *ConsensusPBFT) ExecuteBlock(b *blockChain.BlockValidated) {
	lastBlock, err := blockChain.BlockChain.GetLatestBlock()
	if err != nil {
		logger.Warningf("[Node.Run] ExecuteBlock GetLatestBlock error=%v", err)
		return
	}
	c.EnqueueBlock(*b)
	h := lastBlock.Height + 1
	for _, bb := range c.Blocks {
		if bb.Height < h {
			c.Blocks = c.Blocks[1:]
		} else if bb.Height == h {
			err := blockChain.BlockChain.AddBlock(b)
			if err != nil {
				logger.Warningf("[Node.Run] ExecuteBlock AddBlock error=%v", err)
				break
			}
			c.SendReply(&b.Block)
			c.Blocks = c.Blocks[1:]
		} else {
			break
		}
		h++
	}
	height := h
	for _, msg := range c.BlockMessages {
		if msg.Height < h {
			c.BlockMessages = c.BlockMessages[1:]
			c.ModifyProposalState(&msg)
		} else if msg.Height == h {
			blockChan <- msg
			c.BlockMessages = c.BlockMessages[1:]
		} else {
			break
		}
		h++
	}
	if c.IsLeader() {
		if height > c.UnConfirmedH {
			c.BlockConfirm = true
			if c.MessagePool.Size() >= 200 {
				c.generateBlock()
			}
		}
	}
}

func (c *ConsensusPBFT) ModifyProposalState(msg *model.BlockMessage) {
	for _, p := range msg.AbandonedProposal {
		c.ProposalsCache[string(p.Id)] = drop
	}
	for _, p := range msg.ProposalMessages {
		c.ProposalsCache[string(p.Id)] = keep
	}
}

func (*ConsensusPBFT) SendReply(b *blockChain.Block) {
	l := 0
	for _, p := range b.ProposalMessages {
		msg, err := messages.NewProposalReplyMessage(p.Id)
		if err != nil {
			logger.Warningf("[SendReply] NewProposalReplyMessage error=%v", err)
			continue
		}
		jsonData, err := json.Marshal(msg)
		if err != nil {
			logger.Warningf("[SendReply] json.Marshal error=%v", err)
			continue
		}
		service.Net.SendTo(jsonData, service.ProposalReplyMsg, p.From)
		l++
	}
	fmt.Println("sendreply", len(b.ProposalMessages), l)
}

func (c *ConsensusPBFT) ProcessBlockMessage(msg *model.BlockMessage) {
	id, err := msg.Block.Hash()
	if err != nil {
		logger.Warningf("[Node.Run] block.Hash error=%v", err)
		return
	}
	switch c.ValidateBlock(msg) {
	case dataSync:
		return
	case invalid:
		logger.Warningf("[Node.Run] block is invalid")
		return
	}
	c.Block[string(id)] = *msg
	c.ModifyProposalState(msg)
	if _, ok1 := c.BlockCommitMsg[string(id)]; ok1 && service2.CertificateAuthorityX509.Check(len(c.BlockCommitMsg[string(id)])) {
		blockValidated := blockChain.NewBlockValidated(c.Block[string(id)].Block, c.BlockCommitMsg[string(id)])
		if blockValidated == nil {
			logger.Warningf("[Node.Run] NewBlockValidated failed")
			return
		}
		fmt.Println("Road", 2)
		c.ExecuteBlock(blockValidated)
		delete(c.BlockCommitMsg, string(id))
		delete(c.BlockPrepareMsg, string(id))
		delete(c.Block, string(id))
	} else if _, ok2 := c.BlockPrepareMsg[string(id)]; ok2 && service2.CertificateAuthorityX509.Check(len(c.BlockPrepareMsg[string(id)])) &&
		!c.PrepareSent[string(id)] {
		blockCommitMsg, err := messages.NewBlockCommitMessage(c.View, id)
		if err != nil {
			logger.Warningf("[Node.Run] NewBlockCommitMessage error=%v", err)
			return
		}
		jsonData, err := json.Marshal(blockCommitMsg)
		if err != nil {
			logger.Warningf("[Node.Run] json.Marshal error=%v", err)
			return
		}
		c.PrepareSent[string(id)] = true
		service.Net.BroadCast(jsonData, service.BlockCommitMsg)
	} else {
		blockConfirmMsg, err := messages.NewBlockConfirmMessage(c.View, id)
		if err != nil {
			logger.Warningf("[Node.Run] NewBlockConfirmMessage error=%v", err)
			return
		}
		jsonData, err := json.Marshal(blockConfirmMsg)
		if err != nil {
			logger.Warningf("[Node.Run] json.Marshal error=%v", err)
			return
		}
		service.Net.BroadCast(jsonData, service.BlockConfirmMsg)
	}
}

func (c *ConsensusPBFT) generateBlock() {
	if !c.IsLeader() {
		return
	}
	if c.MessagePool.Size() <= 0 {
		fmt.Printf("[Leader.Run] CurrentBlock is empty\n")
		return
	}
	bound := blockChain.BlockMaxSize
	if len(c.MessagePool.ProposalMessages) < blockChain.BlockMaxSize {
		bound = len(c.MessagePool.ProposalMessages)
	}
	validP, abandonedP := CheckProposals(c.MessagePool.ProposalMessages[:bound])
	block, err := blockChain.BlockChain.MineBlock(validP)
	if err != nil {
		logger.Warningf("[Leader.Run] MineBlock error=%v", err)
		return
	}
	blockMessage, err := model.NewBlockMessage(c.View, block, abandonedP)
	if err != nil {
		logger.Warningf("[Leader.Run] NewBlockMessage error=%v", err)
		return
	}
	jsonData, err := json.Marshal(blockMessage)
	if err != nil {
		logger.Warningf("[Leader.Run] json.Marshal error=%v", err)
		return
	}
	service.Net.BroadCast(jsonData, service.BlockMsg)
	c.MessagePool.Clear(bound)
	c.PPPPCount += uint(bound)
	c.BlockConfirm = false
	c.UnConfirmedH = block.Height
	fmt.Println("block broadcast fin", block.Height, len(validP), validP[len(validP)-1].Values)
}

func (c *ConsensusPBFT) GetLeaderNode() (int64, int64) {
	count := make([]int, service2.CertificateAuthorityX509.GetNetworkSize())
	for _, msg := range c.InitLeaderMessages {
		for _, id := range msg.NodeIds {
			count[id]++
		}
	}
	for i := int64(len(count) - 1); i >= 0; i-- {
		if service2.CertificateAuthorityX509.Check(count[i]) {
			return i, i
		}
	}
	return -1, -1
}

func (c *ConsensusPBFT) IsLeader() bool {
	return service2.CertificateAuthorityX509.IsLeaderNode(c.LeaderId)
}

func (c *ConsensusPBFT) IsNextLeader() bool {
	return service2.CertificateAuthorityX509.IsLeaderNode((c.View + 1) %
		int64(service2.CertificateAuthorityX509.GetNetworkSize()))
}

func (c *ConsensusPBFT) GetLatestBlock() (block model.BlockMessage, proofs map[string][]byte) {
	var h uint
	for _, b := range c.ViewChangeMsgs {
		if h == 0 || h < b.BlockHeader.Height {
			h = b.BlockHeader.Height
			block = b.Block
			proofs = b.Proofs
		}
	}
	return
}


func (c *ConsensusPBFT) GetRecallBlock(h uint) model.BlockMessage {
	for k, b := range c.ViewChangeMsgs {
		if h == b.Block.Height {
			return c.ViewChangeMsgs[k].Block
		}
	}
	return model.BlockMessage{}
}

func (c *ConsensusPBFT) FinChange() {
	c.OnChange = false
}

func (c *ConsensusPBFT) IsOnChanging() bool {
	return c.OnChange
}

func (c *ConsensusPBFT) GetLeaderId() int64 {
	return c.LeaderId
}

func (c *ConsensusPBFT) StartViewChange() {
	var block model.BlockMessage
	lastBlock, err := blockChain.BlockChain.GetLatestBlock()
	if err != nil {
		logger.Warningf("[Node.Run] ProposalConfirm GetLatestBlock error=%v", err)
		return
	}
	for _, b := range c.Block {
		if b.Height == lastBlock.Height+1 {
			block = b
		}
	}
	viewChangeMsg, err := model.NewViewChangeMessage(lastBlock, c.View, &block)
	jsonData, err := json.Marshal(viewChangeMsg)
	if err != nil {
		logger.Warningf("[Node.Run] ProposalConfirm json.Marshal error=%v", err)
		return
	}
	service.Net.BroadCast(jsonData, service.ViewChangeMsg)
}

func (c *ConsensusPBFT) StartChange() {
	c.OnChange = true
	if c.IsNextLeader() {
		block, proofs := c.GetLatestBlock()
		if block.TimeStamp == 0 {
			logger.Warningf("[View.Run] StartChange NewBlockMessage can't find correct block")
			return
		}
		newViewMsg, err := model.NewNewViewMessage(c.View, c.ViewChangeMsgs, block, proofs)
		if err != nil {
			logger.Warningf("[View.Run] StartChange NewNewViewMessage error=%v", err)
			return
		}
		jsonData, err := json.Marshal(newViewMsg)
		if err != nil {
			logger.Warningf("[View.Run] StartChange json.Marshal error=%v", err)
			return
		}
		service.Net.BroadCast(jsonData, service.NewViewMsg)
	}
}

func (c *ConsensusPBFT) ProcessNewViewMsg(msg *model.NewViewMessage) {
	c.ProposalsCache = make(map[string]uint8)
	c.BlockMessages = c.BlockMessages[:0]
	c.Block = make(map[string]model.BlockMessage)
	c.BlockPrepareMsg = make(map[string]map[string][]byte)
	c.ViewChangeMsgs = make(map[string]model.ViewChangeMessage)
	if c.IsLeader() {
		c.MessagePool = messages.NewProposalMessagePool()
		c.BlockConfirm = true
	}
	c.View = msg.View
	c.LeaderId = c.View % int64(service2.CertificateAuthorityX509.GetNetworkSize())
	lastBlock, err := blockChain.BlockChain.GetLatestBlock()
	if err != nil {
		logger.Warningf("[View.Run] ProcessNewViewMsg GetLatestBlock error=%v", err)
		return
	}
	if lastBlock.Height < msg.Height {
		StartDataSync(lastBlock.Height+1, msg.Height)
		if msg.BlockMsg.TimeStamp != 0 {
			c.EnqueueBlockMessage(&msg.BlockMsg)
		}
	} else if lastBlock.Height > msg.Height {
		for {
			err = blockChain.BlockChain.RevokeBlock()
			if err == nil {
				break
			}
			logger.Warningf("[View.Run] ProcessNewViewMsg GetLatestBlock error=%v", err)
		}
	}
	c.FinChange()
	logger.Warningf("[View.Run] ViewChange finish")
}

func CheckProposals(proposals messages.ProposalMessages) (
	messages.ProposalMessages, messages.ProposalMessages) {
	filter := make(map[string]messages.ProposalMessages)
	abandoneP := messages.ProposalMessagePool{}
	validP := messages.ProposalMessagePool{}
	for _, p := range proposals {
		if fp, ok := filter[p.ZoneName]; !ok {
			filter[p.ZoneName] = append(filter[p.ZoneName], p)
			validP.AddProposal(p)
		} else {
			drop := false
			for _, tmpP := range filter[p.ZoneName] {
				if reflect.DeepEqual(p.Id, tmpP.Id) {
					drop = true
					break
				}
			}
			if !drop {
				//TODO: Two conflicted proposal
				tmpP := fp[len(fp)-1]
				switch p.Type {
				case messages.Add:
					if tmpP.Owner != messages.Dereliction {
						abandoneP.AddProposal(p)
					} else {
						validP.AddProposal(p)
					}
				case messages.Mod:
					if tmpP.Owner != p.Owner || tmpP.Owner != p.From {
						abandoneP.AddProposal(p)
					} else {
						validP.AddProposal(p)
					}
				case messages.Del:
					if p.Owner != messages.Dereliction || tmpP.Owner != p.From {
						abandoneP.AddProposal(p)
					} else {
						validP.AddProposal(p)
					}
				}
			}
		}
	}
	return validP.ProposalMessages, abandoneP.ProposalMessages
}

func ValidateProposals(msg *model.BlockMessage) bool {
	tmpPool := messages.ProposalMessages{}
	tmpPool = append(tmpPool, msg.ProposalMessages...)
	tmpPool = append(tmpPool, msg.AbandonedProposal...)
	validP, _ := CheckProposals(tmpPool)
	return reflect.DeepEqual(validP, msg.ProposalMessages)
}

func StartDataSync(lastH, h uint) {
	for i := lastH; i <= h; i++ {
		syncMsg, err := blockChain.NewDataSyncMessage(i)
		if err != nil {
			logger.Warningf("[DataSync] NewDataSyncMessage error=%v", err)
			continue
		}
		jsonData, err := json.Marshal(syncMsg)
		if err != nil {
			logger.Warningf("[DataSync] json.Marshal error=%v", err)
			continue
		}
		service.Net.BroadCast(jsonData, service.DataSyncMsg)
	}
}
