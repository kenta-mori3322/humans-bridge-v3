package common

import (
	"math/big"
	"sort"

	"github.com/humansdotai/humans/common/cosmos"
)

// Gas coins
type Gas Coins

var (
	bnbSingleTxFee = cosmos.NewUint(37500)
	bnbMultiTxFee  = cosmos.NewUint(30000)
	ethTransferFee = cosmos.NewUint(21000)
	ethGasPerByte  = cosmos.NewUint(68)
)

// ETHGasFeeTransfer gas fee for ETH
var ETHGasFeeTransfer = Gas{
	{Asset: ETHAsset, Amount: ethTransferFee},
}

// GetETHGasFee return the gas for ETH
func GetETHGasFee(gasPrice *big.Int, msgLen uint64) Gas {
	gasBytes := ethGasPerByte.MulUint64(msgLen)
	return Gas{
		{Asset: ETHAsset, Amount: ethTransferFee.Add(gasBytes).Mul(cosmos.NewUintFromBigInt(gasPrice))},
	}
}

// MakeETHGas return the gas for ETH
func MakeETHGas(gasPrice *big.Int, gas uint64) Gas {
	gasAmt := cosmos.NewUint(gas).Mul(cosmos.NewUintFromBigInt(gasPrice)).QuoUint64(One * 100)
	return Gas{
		{Asset: ETHAsset, Amount: gasAmt},
	}
}

// Valid return nil when it is valid, otherwise return an error
func (g Gas) Valid() error {
	for _, coin := range g {
		if err := coin.Valid(); err != nil {
			return err
		}
	}

	return nil
}

// IsEmpty return true as long as there is one coin in it that is not empty
func (g Gas) IsEmpty() bool {
	if len(g) == 0 {
		return true
	}
	for _, coin := range g {
		if !coin.IsEmpty() {
			return false
		}
	}
	return true
}

// Add combines two gas objects into one, adding amounts where needed
// or appending new coins.
func (g Gas) Add(g2 Gas) Gas {
	var newGasCoins Gas
	for _, gc2 := range g2 {
		matched := false
		for i, gc1 := range g {
			if gc1.Asset.Equals(gc2.Asset) {
				g[i].Amount = g[i].Amount.Add(gc2.Amount)
				matched = true
			}
		}
		if !matched {
			newGasCoins = append(newGasCoins, gc2)
		}
	}

	return append(g, newGasCoins...)
}

// Sub subtract the given amount gas from existing gas object
func (g Gas) Sub(g2 Gas) Gas {
	for _, gc2 := range g2 {
		for i, gc1 := range g {
			if gc1.Asset.Equals(gc2.Asset) {
				g[i].Amount = SafeSub(g[i].Amount, gc2.Amount)
				break
			}
		}
	}
	return g
}

// Equals Check if two lists of coins are equal to each other. Order does not matter
func (g Gas) Equals(gas2 Gas) bool {
	if len(g) != len(gas2) {
		return false
	}

	// sort both lists
	sort.Slice(g[:], func(i, j int) bool {
		return g[i].Asset.String() < g[j].Asset.String()
	})
	sort.Slice(gas2[:], func(i, j int) bool {
		return gas2[i].Asset.String() < gas2[j].Asset.String()
	})

	for i := range g {
		if !g[i].Equals(gas2[i]) {
			return false
		}
	}

	return true
}

// ToCoins convert the gas to Coins
func (g Gas) ToCoins() Coins {
	coins := make(Coins, len(g))
	for i := range g {
		coins[i] = NewCoin(g[i].Asset, g[i].Amount)
	}
	return coins
}
