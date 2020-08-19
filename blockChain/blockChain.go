package blockChain

import (
	"bytes"
	"errors"
	"fmt"

	"BCDns_0.1/dao"
	"BCDns_0.1/messages"
	"BCDns_0.1/utils"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/boltdb/bolt"
)

const dbFile = "/go/src/BCDns_0.1/bcDns/data/blockchain_%s.db"
const blocksBucket = "blocks"

var (
	BlockChain   *Blockchain
	NewBlockChan chan int
)

// Blockchain implements interactions with a DB
type Blockchain struct {
	tip []byte
	db  *bolt.DB
}

// CreateBlockchain creates a new blockchain DB
func CreateBlockchain(dbFile string) (*Blockchain, error) {
	var tip []byte

	genesis := NewGenesisBlock()
	genesisB := NewBlockValidated(*genesis, map[string][]byte{})

	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		fmt.Printf("[CreateBlockchain] error=%v\n", err)
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucket([]byte(blocksBucket))
		if err != nil {
			return err
		}

		bBytes, err := genesisB.MarshalBlock()
		if err != nil {
			fmt.Printf("[CreateBlockchain] genesis.MarshalBinary error=%v\n", err)
			return err
		}
		key, err := genesisB.Hash()
		if err != nil {
			return err
		}
		err = b.Put(key, bBytes)
		if err != nil {
			return err
		}

		err = b.Put([]byte("l"), key)
		if err != nil {
			return err
		}
		tip = key

		return nil
	})
	if err != nil {
		fmt.Printf("[CreateBlockchain] error=%v\n", err)
		return nil, err
	}

	bc := Blockchain{tip, db}
	LoadBlockchainCache(&bc)

	return &bc, nil
}

// NewBlockchain creates a new Blockchain with genesis Block
func NewBlockchain(nodeID string) (*Blockchain, error) {
	NewBlockChan = make(chan int)
	dbFile := fmt.Sprintf(dbFile, nodeID)
	if utils.DBExists(dbFile) == false {
		fmt.Println("[NewBlockchain]Blockchain is not exists.")
		return CreateBlockchain(dbFile)
	}
	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		tip = b.Get([]byte("l"))

		return nil
	})
	if err != nil {
		return nil, err
	}

	bc := Blockchain{tip, db}
	LoadBlockchainCache(&bc)

	return &bc, nil
}

func (bc *Blockchain) Close() {
	_ = bc.db.Close()
}

// AddBlock saves the block into the blockchain
func (bc *Blockchain) AddBlock(block *BlockValidated) error {
	dao.Dao.Mutex.Lock()
	defer dao.Dao.Mutex.Unlock()
	for _, p := range block.ProposalMessages {
		err := dao.Db.Delete([]byte(p.ZoneName), nil)
		if err != nil {
			fmt.Printf("[AddBlock] Db.Delete error=%v\n", err)
			return err
		}
		ZoneStatePool.Modify(p.ZoneName)
	}

	err := bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		key, err := block.Hash()
		if err != nil {
			return err
		}
		blockInDb := b.Get(key)
		if blockInDb != nil {
			return nil
		}

		for _, p := range block.ProposalMessages {
			b.Put([]byte(p.ZoneName), key)
		}

		blockData, err := block.MarshalBlockValidated()
		if err != nil {
			return err
		}
		err = b.Put(key, blockData)
		if err != nil {
			return err
		}

		lastHash := b.Get([]byte("l"))
		lastBlockData := b.Get(lastHash)
		lastBlock, err := UnMarshalBlockValidated(lastBlockData)
		if err != nil {
			return err
		}

		if block.Height > lastBlock.Height {
			err = b.Put([]byte("l"), key)
			if err != nil {
				return err
			}
			bc.tip = key
		}

		return nil
	})
	if err != nil {
		fmt.Printf("[AddBlock] error=%v\n", err)
		return err
	}

	bcc := GetBlockchainCache(bc)
	bcc.UpdateBlockchainCache(block)
	NewBlockChan <- 1
	return nil
}

func (bc *Blockchain) GetProposalByZoneName(ZoneName string) (*messages.ProposalMessage, error) {
	gp := new(messages.ProposalMessage)
	gerr := errors.New("Not found")

	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		blockHash := b.Get([]byte(ZoneName))
		blockData := b.Get(blockHash)
		block, err := UnMarshalBlockValidated(blockData)
		if err != nil {
			return err
		}
		for _, p := range block.ProposalMessages {
			if p.ZoneName == ZoneName && block.VerifyBlockValidated() {
				gp = &p
				return nil
			}
		}
		return gerr
	})
	if err != nil {
		return nil, err
	}

	return gp, nil
}

// FindTransaction finds a transaction by its ID
func (bc *Blockchain) FindProposal(ID []byte) (messages.ProposalMessage, error) {
	bci := bc.Iterator()

	for {
		block, err := bci.Next()
		if err != nil {
			return messages.ProposalMessage{}, err
		}

		for _, p := range block.ProposalMessages {
			if bytes.Compare(p.Id, ID) == 0 {
				return p, nil
			}
		}

		if len(block.PrevBlock) == 0 {
			break
		}
	}

	return messages.ProposalMessage{}, errors.New("Transaction is not found")
}

// Iterator returns a BlockchainIterat
func (bc *Blockchain) Iterator() *Iterator {
	bci := &Iterator{bc.tip, bc.db}

	return bci
}

// GetLatestBlock returns the latest block
func (bc *Blockchain) GetLatestBlock() (*BlockValidated, error) {
	lastBlock := new(BlockValidated)
	var err error

	err = bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash := b.Get([]byte("l"))
		blockData := b.Get(lastHash)
		lastBlock, err = UnMarshalBlockValidated(blockData)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return lastBlock, nil
}

func (bc *Blockchain) RevokeBlock() error {
	lastBlock, err := bc.GetLatestBlock()
	if err != nil {
		return err
	}
	err = bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		prevHash := lastBlock.PrevBlock
		key, err := lastBlock.Hash()
		if err != nil {
			return err
		}

		err = b.Put([]byte("l"), prevHash)
		if err != nil {
			return err
		}
		bc.tip = prevHash

		err = b.Delete(key)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// GetBlockByHash finds a block by its hash and returns it
func (bc *Blockchain) GetBlockByHash(blockHash []byte) (*BlockValidated, error) {
	block := new(BlockValidated)
	var err error

	err = bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		blockData := b.Get(blockHash)

		if blockData == nil {
			return errors.New("Block is not found.")
		}

		block, err = UnMarshalBlockValidated(blockData)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return block, nil
}

// GetBlockByHeight finds a block by its height and returns it
func (bc *Blockchain) GetBlockByHeight(h uint) (*BlockValidated, error) {
	lastBlock, err := bc.GetLatestBlock()
	if err != nil {
		return nil, err
	}
	if lastBlock.Height < h {
		return nil, errors.New("[GetBlockByHeight] %v out of height")
	}
	bci := bc.Iterator()

	for {
		block, err := bci.Next()
		if err != nil {
			return nil, err
		}
		if block.Height == h {
			return block, nil
		}

	}
}

// GetBlockHashes returns a list of hashes of all the blocks in the chain
func (bc *Blockchain) GetBlockHashes() [][]byte {
	var blocks [][]byte
	bci := bc.Iterator()

	for {
		block, err := bci.Next()
		if err != nil {
			return nil
		}

		hash, err := block.Hash()
		if err != nil {
			continue
		}
		blocks = append(blocks, hash)

		if len(block.PrevBlock) == 0 {
			break
		}
	}

	return blocks
}

// MineBlock mines a new block with the provided transactions
func (bc *Blockchain) MineBlock(proposals messages.ProposalMessages) (*Block, error) {
	block, err := bc.GetLatestBlock()
	if err != nil {
		return nil, err
	}
	lastHash, err := block.Hash()
	if err != nil {
		return nil, err
	}

	newBlock := NewBlock(proposals, lastHash, block.Height+1, false)
	if newBlock == nil {
		return nil, errors.New("[MineBlock] NewBlock failed")
	}

	return newBlock, nil
}

func (bc *Blockchain) FindDomain(name string) (*messages.ProposalMessage, error) {
	bci := bc.Iterator()

	for {
		block, err := bci.Next()
		if err != nil {
			return nil, err
		}

		if p := block.ProposalMessages.FindByZoneName(name); p != nil {
			return p, nil
		}

		if len(block.PrevBlock) == 0 {
			break
		}
	}
	return nil, nil
}

func (bc *Blockchain) Get(key []byte) ([]byte, error) {
	bci := bc.Iterator()

	for {
		block, err := bci.Next()
		if err != nil {
			return nil, err
		}
		ps := ReverseSlice(block.ProposalMessages)
		for _, p := range ps {
			if p.ZoneName == string(key) {
				data, err := p.MarshalProposalMessage()
				if err != nil {
					return nil, err
				}
				return data, nil
			}
		}

		if len(block.PrevBlock) == 0 {
			break

		}
	}
	return nil, leveldb.ErrNotFound
}

func (bc *Blockchain) Set(key, value []byte) error {
	return nil
}

func ReverseSlice(s messages.ProposalMessages) messages.ProposalMessages {
	ss := make(messages.ProposalMessages, len(s))
	for i, j := 0, len(s)-1; i <= j; i, j = i+1, j-1 {
		ss[i], ss[j] = s[j], s[i]
	}
	return ss
}
