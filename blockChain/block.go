package blockChain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"BCDns_0.1/bcDns/conf"
	"BCDns_0.1/certificateAuthority/service"
	"BCDns_0.1/messages"
	"BCDns_0.1/utils"
)

// BlockMaxSize :size of block
const BlockMaxSize = 1

// BlockSlice ?
type BlockSlice []Block

// PreviousBlock ?
func (bs BlockSlice) PreviousBlock() *Block {
	l := len(bs)
	if l == 0 {
		return nil
	} else {
		return &bs[l-1]
	}
}

// Block ：raw block
type Block struct {
	BlockHeader
	messages.ProposalMessages
}

// BlockHeader :block header
type BlockHeader struct {
	PrevBlock   []byte
	MerkelRoot  []byte
	Timestamp   int64
	Height      uint
	MasterChain bool
}

// NewBlock :
func NewBlock(proposals messages.ProposalMessages,
	previousBlock []byte, height uint, masterChain bool, genesis bool) *Block {

	t := time.Now().Unix()
	if genesis {
		t = 0
	}
	header := BlockHeader{
		PrevBlock:   previousBlock,
		Height:      height,
		Timestamp:   t,
		MasterChain: masterChain,
	}
	b := &Block{header, proposals}
	b.MerkelRoot = b.GenerateMerkelRoot()
	return b
}

// NewGenesisBlock :
func NewGenesisBlock() *Block {
	return NewBlock(messages.ProposalMessages{}, []byte{}, 0, true, true)
}

// VerifyBlock : verify block by merkeltree
func (b *Block) VerifyBlock() bool {
	merkel := b.GenerateMerkelRoot()
	return bytes.Compare(merkel, b.MerkelRoot) == 0
}

// Hash :
func (b *Block) Hash() ([]byte, error) {
	headerHash, err := b.BlockHeader.MarshalBlockHeader()
	if err != nil {
		return nil, err
	}
	return utils.SHA256(headerHash), nil
}

// GenerateMerkelRoot :
func (b *Block) GenerateMerkelRoot() []byte {
	var merkell func(hashes [][]byte) []byte
	merkell = func(hashes [][]byte) []byte {

		l := len(hashes)
		if l == 0 {
			return nil
		}
		if l == 1 {
			return hashes[0]
		} else {
			if l%2 == 1 {
				return merkell([][]byte{merkell(hashes[:l-1]), hashes[l-1]})
			}
			bs := make([][]byte, l/2)
			for i, _ := range bs {
				j, k := i*2, (i*2)+1
				bs[i] = utils.SHA256(append(hashes[j], hashes[k]...))
			}
			return merkell(bs)
		}
	}

	ts, ok := Map(func(t messages.ProposalMessage) ([]byte, error) { return t.Id, nil },
		[]messages.ProposalMessage(b.ProposalMessages)).([][]byte)
	if !ok {
		return nil
	}
	return merkell(ts)

}

// MarshalBlock :
func (b *Block) MarshalBlock() ([]byte, error) {
	data, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// UnmarshalBlock :
func UnmarshalBlock(d []byte) (*Block, error) {
	b := new(Block)
	err := json.Unmarshal(d, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalBlockHeader :
func (h *BlockHeader) MarshalBlockHeader() ([]byte, error) {
	jsonData, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	return jsonData, nil
}

// UnmarshalBlockHeader :
func UnmarshalBlockHeader(d []byte) (*BlockHeader, error) {
	b := new(BlockHeader)
	err := json.Unmarshal(d, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Map :
func Map(f interface{}, vs interface{}) interface{} {

	vf := reflect.ValueOf(f)
	vx := reflect.ValueOf(vs)

	l := vx.Len()

	tys := reflect.SliceOf(vf.Type().Out(0))
	vys := reflect.MakeSlice(tys, l, l)

	for i := 0; i < l; i++ {

		y := vf.Call([]reflect.Value{vx.Index(i)})
		vys.Index(i).Set(y[0])
	}

	return vys.Interface()
}

// BlockValidated :
type BlockValidated struct {
	Block
	Signatures map[string][]byte
}

// NewBlockValidated :
func NewBlockValidated(b Block, signatures map[string][]byte) *BlockValidated {
	msg := &BlockValidated{
		Block:      b,
		Signatures: signatures,
	}
	return msg
}

// MarshalBlockValidated :
func (b *BlockValidated) MarshalBlockValidated() ([]byte, error) {
	hash, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// VerifyBlockValidated :
func (b *BlockValidated) VerifyBlockValidated() bool {
	hash, err := b.Block.Hash()
	count := 0
	if err != nil {
		return false
	}

	for id, sign := range b.Signatures {
		if ok := service.CertificateAuthorityX509.VerifySignature(sign, hash, id); !ok {
			continue
		}
		count++
		if ok := service.CertificateAuthorityX509.Checkf(count); ok {
			return true
		}
	}
	return false
}

// UnMarshalBlockValidated :
func UnMarshalBlockValidated(data []byte) (*BlockValidated, error) {
	b := new(BlockValidated)
	err := json.Unmarshal(data, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// DataSyncMessage :
type DataSyncMessage struct {
	utils.Base
	Height    uint
	Signature []byte
}

// NewDataSyncMessage :
func NewDataSyncMessage(h uint) (DataSyncMessage, error) {
	msg := DataSyncMessage{
		Base: utils.Base{
			From:      conf.BCDnsConfig.HostName,
			TimeStamp: time.Now().Unix(),
		},
		Height: h,
	}
	err := msg.Sign()
	if err != nil {
		return DataSyncMessage{}, err
	}
	return msg, nil
}

// Hash :
func (msg *DataSyncMessage) Hash() ([]byte, error) {
	buf := bytes.Buffer{}
	if jsonData, err := json.Marshal(msg.Base); err != nil {
		return nil, err
	} else {
		buf.Write(jsonData)
	}
	if jsonData, err := json.Marshal(msg.Height); err != nil {
		return nil, err
	} else {
		buf.Write(jsonData)
	}
	return utils.SHA256(buf.Bytes()), nil
}

// Sign :
func (msg *DataSyncMessage) Sign() error {
	hash, err := msg.Hash()
	if err != nil {
		return err
	}
	if sig := service.CertificateAuthorityX509.Sign(hash); sig != nil {
		msg.Signature = sig
		return nil
	}
	return errors.New("[DataSyncMessage] Generate signature failed")
}

// VerifySignature :
func (msg *DataSyncMessage) VerifySignature() bool {
	hash, err := msg.Hash()
	if err != nil {
		return false
	}
	return service.CertificateAuthorityX509.VerifySignature(msg.Signature, hash, msg.From)
}

// DataSyncRespMessage :
type DataSyncRespMessage struct {
	utils.Base
	BlockValidated
	Signature []byte
}

// NewDataSyncRespMessage :
func NewDataSyncRespMessage(b *BlockValidated) (DataSyncRespMessage, error) {
	msg := DataSyncRespMessage{
		Base: utils.Base{
			From:      conf.BCDnsConfig.HostName,
			TimeStamp: time.Now().Unix(),
		},
		BlockValidated: *b,
	}
	err := msg.Sign()
	if err != nil {
		return DataSyncRespMessage{}, err
	}
	return msg, nil
}

// Hash :
func (msg *DataSyncRespMessage) Hash() ([]byte, error) {
	buf := bytes.Buffer{}
	bHash, err := msg.BlockValidated.Hash()
	if err != nil {
		return nil, err
	}
	buf.Write(bHash)
	if jsonData, err := json.Marshal(msg.Base); err != nil {
		return nil, err
	} else {
		buf.Write(jsonData)
	}
	return utils.SHA256(buf.Bytes()), nil
}

// Sign :
func (msg *DataSyncRespMessage) Sign() error {
	hash, err := msg.Hash()
	if err != nil {
		return err
	}
	if sig := service.CertificateAuthorityX509.Sign(hash); sig != nil {
		msg.Signature = sig
		return nil
	}
	return errors.New("[DataSyncRespMessage] Generate signature failed")
}

// VerifySignature :
func (msg *DataSyncRespMessage) VerifySignature() bool {
	hash, err := msg.Hash()
	if err != nil {
		return false
	}
	return service.CertificateAuthorityX509.VerifySignature(msg.Signature, hash, msg.From)
}

// Validate :
func (msg *DataSyncRespMessage) Validate() bool {
	hash, err := msg.Hash()
	if err != nil {
		return false
	}
	if !service.CertificateAuthorityX509.VerifySignature(msg.Signature, hash, msg.From) {
		return false
	}
	headerHash, err := msg.BlockHeader.MarshalBlockHeader()
	if err != nil {
		return false
	}
	id := utils.SHA256(headerHash)
	count := len(msg.Signatures)
	for host, sig := range msg.Signatures {
		if !service.CertificateAuthorityX509.VerifySignature(sig, id, host) {
			fmt.Println("???", host, sig)
			count--
			if !service.CertificateAuthorityX509.Check(count) {
				return false
			}
		}
	}
	return true
}
