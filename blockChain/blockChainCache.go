package blockChain

import (
	"BCDns_0.1/messages"
)

var bcc *BlockchainCache

type BlockchainCache struct {
	BlockCache []BlockValidated
	//	ZoneNameMap map[string]*BlockValidated
	ZoneNameCache map[string]*messages.ProposalMessage
	RRsCache      map[string]*BlockValidated
}

func GetBlockchainCache(bc *Blockchain) *BlockchainCache {
	if bcc != nil {
		return bcc
	}

	return LoadBlockchainCache(bc)
}

func LoadBlockchainCache(bc *Blockchain) *BlockchainCache {
	bcc = NewBlockchainCache()

	bci := bc.Iterator()
	for {
		block, err := bci.Next()
		if err != nil {
			return nil
		}

		bcc.BlockCache = append(bcc.BlockCache, *block)
		if len(block.PrevBlock) == 0 {
			break
		}
	}
	bcc.ReverseBlock()

	for i := 0; i < len(bcc.BlockCache); i++ {
		bcc.UpdateCache(&bcc.BlockCache[i])
	}
	return bcc
}

func (bcc *BlockchainCache) UpdateBlockchainCache(block *BlockValidated) {
	bcc.BlockCache = append(bcc.BlockCache, *block)
	bcc.UpdateCache(&bcc.BlockCache[len(bcc.BlockCache)-1])
}

func (bcc *BlockchainCache) UpdateCache(block *BlockValidated) {
	for _, p := range (*block).ProposalMessages {
		if p.Type == messages.Del || p.Type == messages.Mod {
			for _, rr := range bcc.ZoneNameCache[p.ZoneName].Values {
				delete(bcc.RRsCache, rr)
			}
		}
		if p.Type == messages.Add || p.Type == messages.Mod {
			for _, rr := range p.Values {
				bcc.RRsCache[rr] = block
			}
		}
		bcc.ZoneNameCache[p.ZoneName] = &p
	}
}

func (bcc *BlockchainCache) RevokeBlock(block *BlockValidated) error {
	//TODO revoke blockcache
	return nil
}

func (bcc *BlockchainCache) ReverseBlock() {
	for i, j := 0, len(bcc.BlockCache)-1; i < j; i, j = i+1, j-1 {
		bcc.BlockCache[i], bcc.BlockCache[j] = bcc.BlockCache[j], bcc.BlockCache[i]
	}
}

func NewBlockchainCache() *BlockchainCache {
	return &BlockchainCache{}
}

func (bcc *BlockchainCache) CheckRRs(rrs []string) bool {
	for _, rr := range rrs {
		if b, ok := bcc.RRsCache[rr]; ok {
			if !b.VerifyBlockValidated() {
				return false
			}
		} else {
			return false
		}
	}
	return true
}
