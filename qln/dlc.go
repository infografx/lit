package qln

import (
	"fmt"
	"math"
	"os"
	"encoding/json"

	"bytes"
	"bufio"

	"github.com/mit-dci/lit/btcutil"
	"github.com/mit-dci/lit/btcutil/txscript"
	"github.com/mit-dci/lit/btcutil/txsort"
	"github.com/mit-dci/lit/crypto/koblitz"
	"github.com/mit-dci/lit/dlc"
	"github.com/mit-dci/lit/lnutil"
	"github.com/mit-dci/lit/logging"
	"github.com/mit-dci/lit/portxo"
	"github.com/mit-dci/lit/sig64"
	"github.com/mit-dci/lit/wire"
)

func (nd *LitNode) AddContract() (*lnutil.DlcContract, error) {

	fmt.Printf("::%s:: AddContract(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c, err := nd.DlcManager.AddContract()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (nd *LitNode) OfferDlc(peerIdx uint32, cIdx uint64) error {


	fmt.Printf("::%s:: OfferDlc(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		return err
	}

	if c.Status != lnutil.ContractStatusDraft {
		return fmt.Errorf("You cannot offer a contract to someone that is not in draft stage")
	}

	if !nd.ConnectedToPeer(peerIdx) {
		return fmt.Errorf("You are not connected to peer %d, do that first", peerIdx)
	}

	var nullBytes [33]byte
	// Check if everything's set
	if c.OracleA == nullBytes {
		return fmt.Errorf("You need to set an oracle for the contract before offering it")
	}

	if c.OracleR == nullBytes {
		return fmt.Errorf("You need to set an R-point for the contract before offering it")
	}

	if c.OracleTimestamp == 0 {
		return fmt.Errorf("You need to set a settlement time for the contract before offering it")
	}

	if c.CoinType == dlc.COINTYPE_NOT_SET {
		return fmt.Errorf("You need to set a coin type for the contract before offering it")
	}

	if c.Division == nil {
		return fmt.Errorf("You need to set a payout division for the contract before offering it")
	}

	if c.OurFundingAmount+c.TheirFundingAmount == 0 {
		return fmt.Errorf("You need to set a funding amount for the peers in contract before offering it")
	}

	c.PeerIdx = peerIdx

	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = c.CoinType | 1<<31
	kg.Step[2] = UseContractFundMultisig
	kg.Step[3] = c.PeerIdx | 1<<31
	kg.Step[4] = uint32(c.Idx) | 1<<31

	c.OurFundMultisigPub, err = nd.GetUsePub(kg, UseContractFundMultisig)
	if err != nil {
		return err
	}

	c.OurPayoutBase, err = nd.GetUsePub(kg, UseContractPayoutBase)
	if err != nil {
		return err
	}

	// Fund the contract
	err = nd.FundContract(c)
	if err != nil {
		return err
	}

	msg := lnutil.NewDlcOfferMsg(peerIdx, c)

	c.Status = lnutil.ContractStatusOfferedByMe
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		return err
	}


	fmt.Printf("::%s:: OfferDlc(): qln/dlc.go: c: %#v \n",os.Args[6][len(os.Args[6])-4:], c)

	nd.tmpSendLitMsg(msg)

	return nil
}

func (nd *LitNode) DeclineDlc(cIdx uint64, reason uint8) error {
	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		return err
	}

	if c.Status != lnutil.ContractStatusOfferedToMe {
		return fmt.Errorf("You cannot decline a contract unless it is in the 'Offered/Awaiting reply' state")
	}

	if !nd.ConnectedToPeer(c.PeerIdx) {
		return fmt.Errorf("You are not connected to peer %d, do that first", c.PeerIdx)
	}

	msg := lnutil.NewDlcOfferDeclineMsg(c.PeerIdx, reason, c.TheirIdx)
	c.Status = lnutil.ContractStatusDeclined

	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		return err
	}

	nd.tmpSendLitMsg(msg)

	return nil
}

func (nd *LitNode) AcceptDlc(cIdx uint64) error {


	fmt.Printf("::%s:: AcceptDlc(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		return err
	}

	if c.Status != lnutil.ContractStatusOfferedToMe {
		return fmt.Errorf("You cannot decline a contract unless it is in the 'Offered/Awaiting reply' state")
	}

	if !nd.ConnectedToPeer(c.PeerIdx) {
		return fmt.Errorf("You are not connected to peer %d, do that first", c.PeerIdx)
	}

	// Preconditions checked - Go execute the acceptance in a separate go routine
	// while returning the status back to the client
	go func(nd *LitNode, c *lnutil.DlcContract) {
		c.Status = lnutil.ContractStatusAccepting
		nd.DlcManager.SaveContract(c)

		// Fund the contract
		err = nd.FundContract(c)
		if err != nil {
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}

		var kg portxo.KeyGen
		kg.Depth = 5
		kg.Step[0] = 44 | 1<<31
		kg.Step[1] = c.CoinType | 1<<31
		kg.Step[2] = UseContractFundMultisig
		kg.Step[3] = c.PeerIdx | 1<<31
		kg.Step[4] = uint32(c.Idx) | 1<<31

		c.OurFundMultisigPub, err = nd.GetUsePub(kg, UseContractFundMultisig)
		if err != nil {
			logging.Errorf("Error while getting multisig pubkey: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}

		c.OurPayoutBase, err = nd.GetUsePub(kg, UseContractPayoutBase)
		if err != nil {
			logging.Errorf("Error while getting payoutbase: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}

		ourPayoutPKHKey, err := nd.GetUsePub(kg, UseContractPayoutPKH)
		if err != nil {
			logging.Errorf("Error while getting our payout pubkey: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}
		copy(c.OurPayoutPKH[:], btcutil.Hash160(ourPayoutPKHKey[:]))

		// Now we can sign the division
		sigs, err := nd.SignSettlementDivisions(c)
		if err != nil {
			logging.Errorf("Error signing settlement divisions: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}

		msg := lnutil.NewDlcOfferAcceptMsg(c, sigs)
		c.Status = lnutil.ContractStatusAccepted

		nd.DlcManager.SaveContract(c)

		fmt.Printf("::%s::AcceptDlc(): qln/dlc.go: c: %#v \n",os.Args[6][len(os.Args[6])-4:], c)

		nd.tmpSendLitMsg(msg)
	}(nd, c)
	return nil
}

func (nd *LitNode) DlcOfferHandler(msg lnutil.DlcOfferMsg, peer *RemotePeer) {

	fmt.Printf("::%s:: DlcOfferHandler: qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c := new(lnutil.DlcContract)

	c.PeerIdx = peer.Idx
	c.Status = lnutil.ContractStatusOfferedToMe
	// Reverse copy from the contract we received
	c.OurFundingAmount = msg.Contract.TheirFundingAmount
	c.TheirFundingAmount = msg.Contract.OurFundingAmount
	c.OurFundingInputs = msg.Contract.TheirFundingInputs
	c.TheirFundingInputs = msg.Contract.OurFundingInputs
	c.OurFundMultisigPub = msg.Contract.TheirFundMultisigPub
	c.TheirFundMultisigPub = msg.Contract.OurFundMultisigPub
	c.OurPayoutBase = msg.Contract.TheirPayoutBase
	c.TheirPayoutBase = msg.Contract.OurPayoutBase
	c.OurChangePKH = msg.Contract.TheirChangePKH
	c.TheirChangePKH = msg.Contract.OurChangePKH
	c.TheirIdx = msg.Contract.Idx

	c.Division = make([]lnutil.DlcContractDivision, len(msg.Contract.Division))
	for i := 0; i < len(msg.Contract.Division); i++ {
		c.Division[i].OracleValue = msg.Contract.Division[i].OracleValue
		c.Division[i].ValueOurs = (c.TheirFundingAmount + c.OurFundingAmount) - msg.Contract.Division[i].ValueOurs
	}

	// Copy
	c.CoinType = msg.Contract.CoinType
	c.OracleA = msg.Contract.OracleA
	c.OracleR = msg.Contract.OracleR
	c.OracleTimestamp = msg.Contract.OracleTimestamp

	err := nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcOfferHandler SaveContract err %s\n", err.Error())
		return
	}


	fmt.Printf("::%s:: DlcOfferHandler: qln/dlc.go: c: %#v \n",os.Args[6][len(os.Args[6])-4:], c)


	_, ok := nd.SubWallet[msg.Contract.CoinType]
	if !ok {
		// We don't have this coin type, automatically decline
		nd.DeclineDlc(c.Idx, 0x02)
	}

}

func (nd *LitNode) DlcDeclineHandler(msg lnutil.DlcOfferDeclineMsg, peer *RemotePeer) {
	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcDeclineHandler FindContract err %s\n", err.Error())
		return
	}

	c.Status = lnutil.ContractStatusDeclined
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcDeclineHandler SaveContract err %s\n", err.Error())
		return
	}
}

func (nd *LitNode) DlcAcceptHandler(msg lnutil.DlcOfferAcceptMsg, peer *RemotePeer) error {

	fmt.Printf("::%s:: DlcAcceptHandler: qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcAcceptHandler FindContract err %s\n", err.Error())
		return err
	}

	// TODO: Check signatures

	c.TheirChangePKH = msg.OurChangePKH
	c.TheirFundingInputs = msg.FundingInputs
	c.TheirSettlementSignatures = msg.SettlementSignatures
	c.TheirFundMultisigPub = msg.OurFundMultisigPub
	c.TheirPayoutBase = msg.OurPayoutBase
	c.TheirPayoutPKH = msg.OurPayoutPKH
	c.TheirIdx = msg.OurIdx

	c.Status = lnutil.ContractStatusAccepted
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcAcceptHandler SaveContract err %s\n", err.Error())
		return err
	}

	// create our settlement signatures and ack
	sigs, err := nd.SignSettlementDivisions(c)
	if err != nil {
		return err
	}

	outMsg := lnutil.NewDlcContractAckMsg(c, sigs)
	c.Status = lnutil.ContractStatusAcknowledged

	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		return err
	}

	fmt.Printf("::%s:: DlcAcceptHandler: qln/dlc.go: c %#v \n",os.Args[6][len(os.Args[6])-4:], c)

	nd.tmpSendLitMsg(outMsg)

	return nil

}

func (nd *LitNode) DlcContractAckHandler(msg lnutil.DlcContractAckMsg, peer *RemotePeer) {

	fmt.Printf("::%s:: DlcContractAckHandler: qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcContractAckHandler FindContract err %s\n", err.Error())
		return
	}

	// TODO: Check signatures

	c.Status = lnutil.ContractStatusAcknowledged

	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcContractAckHandler SaveContract err %s\n", err.Error())
		return
	}

	// We have everything now, send our signatures to the funding TX
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		logging.Errorf("DlcContractAckHandler No wallet for cointype %d\n", c.CoinType)
		return
	}

	tx, err := nd.BuildDlcFundingTransaction(c)
	if err != nil {
		logging.Errorf("DlcContractAckHandler BuildDlcFundingTransaction err %s\n", err.Error())
		return
	}

	err = wal.SignMyInputs(&tx)
	if err != nil {
		logging.Errorf("DlcContractAckHandler SignMyInputs err %s\n", err.Error())
		return
	}

	outMsg := lnutil.NewDlcContractFundingSigsMsg(c, &tx)

	fmt.Printf("::%s:: DlcContractAckHandler: qln/dlc.go: c %#v \n",os.Args[6][len(os.Args[6])-4:], c)

	nd.tmpSendLitMsg(outMsg)
}

func (nd *LitNode) DlcFundingSigsHandler(msg lnutil.DlcContractFundingSigsMsg, peer *RemotePeer) {

	fmt.Printf("::%s:: DlcFundingSigsHandler(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcFundingSigsHandler FindContract err %s\n", err.Error())
		returnа
	}

	// TODO: Check signatures

	// We have everything now. Sign our inputs to the funding TX and send it to the blockchain.
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		logging.Errorf("DlcFundingSigsHandler No wallet for cointype %d\n", c.CoinType)
		return
	}

	wal.SignMyInputs(msg.SignedFundingTx)


	fmt.Printf("::%s::DlcFundingSigsHandler()::DirectSendTx::qln/dlc.go\n", os.Args[6][len(os.Args[6])-4:])


	in_wit_size := 0
	for _, intx := range msg.SignedFundingTx.TxIn {

		in_wit_size += intx.Witness.SerializeSize()
		fmt.Printf("::%s:: in_wit_size : %d \n",os.Args[6][len(os.Args[6])-4:], in_wit_size)

	} 



	//================================================================================


	n := 8 + VarIntSerializeSize(uint64(len(msg.SignedFundingTx.TxIn))) +
	VarIntSerializeSize(uint64(len(msg.SignedFundingTx.TxOut)))


	n_out := 0
	for _, outtx := range msg.SignedFundingTx.TxOut {

		n_out += outtx.SerializeSize()
		fmt.Printf("::%s:: n_out : %d \n",os.Args[6][len(os.Args[6])-4:], outtx.SerializeSize())

	}

	n_in := 0
	for _, intx := range msg.SignedFundingTx.TxIn {

		n_in += intx.SerializeSize()
		fmt.Printf("::%s:: n_in : %d \n",os.Args[6][len(os.Args[6])-4:], intx.SerializeSize())

	}



	fmt.Printf("::%s:: DlcFundingSigsHandler()::qln/dlc.go: n: %d \n",os.Args[6][len(os.Args[6])-4:], n)			// ?
	fmt.Printf("::%s:: DlcFundingSigsHandler()::qln/dlc.go: n_out: %d \n",os.Args[6][len(os.Args[6])-4:], n_out)	// ?
	fmt.Printf("::%s:: DlcFundingSigsHandler()::qln/dlc.go: n_in: %d \n",os.Args[6][len(os.Args[6])-4:], n_in)		// ?	



	fmt.Printf("::%s:: DlcFundingSigsHandler()::qln/dlc.go: msg.SignedFundingTx.SerializeSize() : %d \n",os.Args[6][len(os.Args[6])-4:], msg.SignedFundingTx.SerializeSize())    // ?
	fmt.Printf("::%s:: DlcFundingSigsHandler()::qln/dlc.go: msg.SignedFundingTx.SerializeSizeStripped() : %d \n",os.Args[6][len(os.Args[6])-4:], msg.SignedFundingTx.SerializeSizeStripped())  // ?



	ctxvsize := (msg.SignedFundingTx.SerializeSizeStripped() * 3 + msg.SignedFundingTx.SerializeSize())/4

	fmt.Printf("::%s::DlcFundingSigsHandler()::qln/dlc.go: msg.SignedFundingTx vsize %d \n", os.Args[6][len(os.Args[6])-4:], ctxvsize)  // ?. 252 from the blockchain. good.


	//=================================================================================


	wal.DirectSendTx(msg.SignedFundingTx)

	err = wal.WatchThis(c.FundingOutpoint)
	if err != nil {
		logging.Errorf("DlcFundingSigsHandler WatchThis err %s\n", err.Error())
		return
	}

	c.Status = lnutil.ContractStatusActive
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcFundingSigsHandler SaveContract err %s\n", err.Error())
		return
	}

	outMsg := lnutil.NewDlcContractSigProofMsg(c, msg.SignedFundingTx)

	nd.tmpSendLitMsg(outMsg)
}

func (nd *LitNode) DlcSigProofHandler(msg lnutil.DlcContractSigProofMsg, peer *RemotePeer) {


	fmt.Printf("::%s:: DlcSigProofHandler(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcSigProofHandler FindContract err %s\n", err.Error())
		return
	}

	// TODO: Check signatures
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		logging.Errorf("DlcSigProofHandler No wallet for cointype %d\n", c.CoinType)
		return
	}

	err = wal.WatchThis(c.FundingOutpoint)
	if err != nil {
		logging.Errorf("DlcSigProofHandler WatchThis err %s\n", err.Error())
		return
	}

	c.Status = lnutil.ContractStatusActive
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcSigProofHandler SaveContract err %s\n", err.Error())
		return
	}
}

func (nd *LitNode) SignSettlementDivisions(c *lnutil.DlcContract) ([]lnutil.DlcContractSettlementSignature, error) {

	fmt.Printf("::%s:: SignSettlementDivisions(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		return nil, fmt.Errorf("Wallet of type %d not found", c.CoinType)
	}

	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = c.CoinType | 1<<31
	kg.Step[2] = UseContractFundMultisig
	kg.Step[3] = c.PeerIdx | 1<<31
	kg.Step[4] = uint32(c.Idx) | 1<<31

	priv, err := wal.GetPriv(kg)
	if err != nil {
		return nil, fmt.Errorf("Could not get private key for contract %d", c.Idx)
	}

	fundingTx, err := nd.BuildDlcFundingTransaction(c)
	if err != nil {
		return nil, err
	}

	c.FundingOutpoint = wire.OutPoint{Hash: fundingTx.TxHash(), Index: 0}

	returnValue := make([]lnutil.DlcContractSettlementSignature, len(c.Division))
	for i, d := range c.Division {
		tx, err := lnutil.SettlementTx(c, d, true)
		if err != nil {
			return nil, err
		}
		sig, err := nd.SignSettlementTx(c, tx, priv)
		if err != nil {
			return nil, err
		}
		returnValue[i].Outcome = d.OracleValue
		returnValue[i].Signature = sig

		fmt.Printf("::%s:: BuildDlcFundingTransaction(): returnValue[i].Outcome %d \n",os.Args[6][len(os.Args[6])-4:], returnValue[i].Outcome)
		fmt.Printf("::%s:: BuildDlcFundingTransaction(): returnValue[i].Signature %x \n",os.Args[6][len(os.Args[6])-4:], returnValue[i].Signature)
	}

	return returnValue, nil
}

func (nd *LitNode) BuildDlcFundingTransaction(c *lnutil.DlcContract) (wire.MsgTx, error) {

	fmt.Printf("::%s:: BuildDlcFundingTransaction(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])

	// make the tx
	tx := wire.NewMsgTx()

	// set version 2, for op_csv
	tx.Version = 2

	// add all the txins
	var ourInputTotal int64
	var theirInputTotal int64

	our_in_size := int(0)
	their_in_size := int(0)

	our_txin_num := 0
	for i, u := range c.OurFundingInputs {
		txin := wire.NewTxIn(&u.Outpoint, nil, nil)

		our_in_size += txin.SerializeSize()

		tx.AddTxIn(txin)
		ourInputTotal += u.Value

		our_txin_num = i
	}
	our_txin_num += 1

	their_txin_num := 0
	for i, u := range c.TheirFundingInputs {
		txin := wire.NewTxIn(&u.Outpoint, nil, nil)

		their_in_size += txin.SerializeSize()

		tx.AddTxIn(txin)
		theirInputTotal += u.Value

		their_txin_num = i
	}
	their_txin_num += 1

	fmt.Printf("::%s:: BuildDlcFundingTransaction(): our_txin_num: %d \n",os.Args[6][len(os.Args[6])-4:], our_txin_num)
	fmt.Printf("::%s:: BuildDlcFundingTransaction(): their_txin_num: %d \n",os.Args[6][len(os.Args[6])-4:], their_txin_num)


	n_in_general := 0
	for _, intx := range tx.TxIn {

		n_in_general += intx.SerializeSize()

	}	


	fmt.Printf("::%s:: BuildDlcFundingTransaction(): n_in_general: %d \n",os.Args[6][len(os.Args[6])-4:], n_in_general)
	fmt.Printf("::%s:: BuildDlcFundingTransaction(): our_in_size: %d \n",os.Args[6][len(os.Args[6])-4:], our_in_size)
	fmt.Printf("::%s:: BuildDlcFundingTransaction(): their_in_size: %d \n",os.Args[6][len(os.Args[6])-4:], their_in_size)


	
	//====================================================

	// Here can be a situation when peers have different number of inputs.
	// Therefore we have to calculate fees for each peer separately.

	// This transaction always will have 3 outputs ( 43 + 31 + 31)
	tx_basesize := 10 + 43 + 31 + 31
	tx_size_foreach := tx_basesize / 2
	tx_size_foreach += 1 // rounding

	input_wit_size := 107

	our_tx_vsize := ((tx_size_foreach + (41 * our_txin_num)) * 3 + (tx_size_foreach + (41 * our_txin_num) + (input_wit_size*our_txin_num) )) / 4
	their_tx_vsize := ((tx_size_foreach + (41 * their_txin_num)) * 3 + (tx_size_foreach + (41 * their_txin_num) + (input_wit_size*their_txin_num) )) / 4

	//rounding
	our_tx_vsize += 1
	their_tx_vsize += 1

	fmt.Printf("::%s:: BuildDlcFundingTransaction(): our_tx_vsize: %d \n",os.Args[6][len(os.Args[6])-4:], our_tx_vsize)
	fmt.Printf("::%s:: BuildDlcFundingTransaction(): their_tx_vsize: %d \n",os.Args[6][len(os.Args[6])-4:], their_tx_vsize)

	our_fee := int64(our_tx_vsize * 80)
	their_fee := int64(their_tx_vsize * 80)

	fmt.Printf("::%s:: BuildDlcFundingTransaction(): our_fee: %d \n",os.Args[6][len(os.Args[6])-4:], our_fee)
	fmt.Printf("::%s:: BuildDlcFundingTransaction(): their_fee: %d \n",os.Args[6][len(os.Args[6])-4:], their_fee)	

	//====================================================

	// add change and sort

	fmt.Printf("::%s:: BuildDlcFundingTransaction(): theirInputTotal-c.TheirFundingAmount-their_fee: %d \n",os.Args[6][len(os.Args[6])-4:], theirInputTotal-c.TheirFundingAmount-500)
	fmt.Printf("::%s:: BuildDlcFundingTransaction(): theirInputTotal: %d, c.TheirFundingAmount: %d \n",os.Args[6][len(os.Args[6])-4:], theirInputTotal, c.TheirFundingAmount)

	their_txout := wire.NewTxOut(theirInputTotal-c.TheirFundingAmount-their_fee, lnutil.DirectWPKHScriptFromPKH(c.TheirChangePKH)) 

	fmt.Printf("::%s:: BuildDlcFundingTransaction(): their_txout Size: %d \n",os.Args[6][len(os.Args[6])-4:], their_txout.SerializeSize())

	tx.AddTxOut(their_txout)

	
	//-----------------------------


	fmt.Printf("::%s:: BuildDlcFundingTransaction(): ourInputTotal-c.OurFundingAmount-our_fee: %d \n",os.Args[6][len(os.Args[6])-4:], ourInputTotal-c.OurFundingAmount-500)
	fmt.Printf("::%s:: BuildDlcFundingTransaction(): ourInputTotal: %d, c.OurFundingAmount: %d \n",os.Args[6][len(os.Args[6])-4:], ourInputTotal, c.OurFundingAmount)

	our_txout := wire.NewTxOut(ourInputTotal-c.OurFundingAmount-our_fee, lnutil.DirectWPKHScriptFromPKH(c.OurChangePKH))

	fmt.Printf("::%s:: BuildDlcFundingTransaction(): our_txout Size: %d \n",os.Args[6][len(os.Args[6])-4:], our_txout.SerializeSize())

	tx.AddTxOut(our_txout)

	


	txsort.InPlaceSort(tx)

	// get txo for channel
	txo, err := lnutil.FundTxOut(c.TheirFundMultisigPub, c.OurFundMultisigPub, c.OurFundingAmount+c.TheirFundingAmount)
	if err != nil {
		return *tx, err
	}

	// Ensure contract funding output is always at position 0
	txos := make([]*wire.TxOut, len(tx.TxOut)+1)
	txos[0] = txo
	copy(txos[1:], tx.TxOut)
	tx.TxOut = txos


	var buft bytes.Buffer
	wtt := bufio.NewWriter(&buft)
	tx.Serialize(wtt)
	wtt.Flush()


	fmt.Printf("::%s:: BuildDlcFundingTransaction(): qln/dlc.go: tx: %x \n",os.Args[6][len(os.Args[6])-4:], buft.Bytes())



	return *tx, nil

}

func (nd *LitNode) FundContract(c *lnutil.DlcContract) error {

	fmt.Printf("::%s:: FundContract(): qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])


	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		return fmt.Errorf("No wallet of type %d connected", c.CoinType)
	}


	fmt.Printf("::%s:: FundContract(): feePerByte: %d, wal.Fee(): %d \n", os.Args[6][len(os.Args[6])-4:], 500, wal.Fee())

	utxos, _, err := wal.PickUtxos(int64(c.OurFundingAmount), 500, wal.Fee(), true)
	if err != nil {
		return err
	}

	c.OurFundingInputs = make([]lnutil.DlcContractFundingInput, len(utxos))
	for i := 0; i < len(utxos); i++ {

		fmt.Printf("::%s:: FundContract(): Value: utxos[i].Value: %d \n", os.Args[6][len(os.Args[6])-4:], utxos[i].Value)

		c.OurFundingInputs[i] = lnutil.DlcContractFundingInput{Outpoint: utxos[i].Op, Value: utxos[i].Value}
	}

	c.OurChangePKH, err = wal.NewAdr()
	if err != nil {
		return err
	}

	return nil
}


// VarIntSerializeSize returns the number of bytes it would take to serialize
// val as a variable length integer.
func VarIntSerializeSize(val uint64) int {
	// The value is small enough to be represented by itself, so it's
	// just 1 byte.
	if val < 0xfd {
		return 1
	}

	// Discriminant 1 byte plus 2 bytes for the uint16.
	if val <= math.MaxUint16 {
		return 3
	}

	// Discriminant 1 byte plus 4 bytes for the uint32.
	if val <= math.MaxUint32 {
		return 5
	}

	// Discriminant 1 byte plus 8 bytes for the uint64.
	return 9
}







//==========================================================================
//==========================================================================
//==========================================================================
//==========================================================================



func (nd *LitNode) SettleContract(cIdx uint64, oracleValue int64, oracleSig [32]byte) ([32]byte, [32]byte, error) {


	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		logging.Errorf("SettleContract FindContract err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}


	cmar, _ := json.Marshal(c)
	fmt.Printf("%s\n", cmar)


	c.Status = lnutil.ContractStatusSettling
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("SettleContract SaveContract err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	d, err := c.GetDivision(oracleValue)
	if err != nil {
		logging.Errorf("SettleContract GetDivision err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		return [32]byte{}, [32]byte{}, fmt.Errorf("SettleContract Wallet of type %d not found", c.CoinType)
	}

	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = c.CoinType | 1<<31
	kg.Step[2] = UseContractFundMultisig
	kg.Step[3] = c.PeerIdx | 1<<31
	kg.Step[4] = uint32(c.Idx) | 1<<31

	priv, err := wal.GetPriv(kg)
	if err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("SettleContract Could not get private key for contract %d", c.Idx)
	}


	fmt.Printf("::%s:: SettleContract(): priv.Serialize() : %d \n",os.Args[6][len(os.Args[6])-4:], priv.Serialize())


	settleTx, err := lnutil.SettlementTx(c, *d, false)

	var buft bytes.Buffer
	wtt := bufio.NewWriter(&buft)
	settleTx.Serialize(wtt)
	wtt.Flush()


	fmt.Printf("::%s:: SettleContract(): After lnutil.SettlementTx(c, *d, false) : %d \n",os.Args[6][len(os.Args[6])-4:], settleTx.SerializeSize())
	fmt.Printf("::%s:: SettleContract(): After lnutil.SettlementTx(c, *d, false) Stripped : %d \n",os.Args[6][len(os.Args[6])-4:], settleTx.SerializeSizeStripped())

	n := 8 + VarIntSerializeSize(uint64(len(settleTx.TxIn))) +
	VarIntSerializeSize(uint64(len(settleTx.TxOut)))


	n_out := 0
	for i1, outtx := range settleTx.TxOut {

		n_out += outtx.SerializeSize()
		fmt.Printf("::%s:: i1 : %d \n",os.Args[6][len(os.Args[6])-4:], i1)

	}

	n_in := 0
	for i2, intx := range settleTx.TxIn {

		n_in += intx.SerializeSize()
		fmt.Printf("::%s:: i2 : %d \n",os.Args[6][len(os.Args[6])-4:], i2)

	}



	fmt.Printf("::%s:: SettleContract(): settleTx: n: %d \n",os.Args[6][len(os.Args[6])-4:], n)
	fmt.Printf("::%s:: SettleContract(): settleTx: n_out: %d \n",os.Args[6][len(os.Args[6])-4:], n_out)
	fmt.Printf("::%s:: SettleContract(): settleTx: n_in: %d \n",os.Args[6][len(os.Args[6])-4:], n_in)

	fmt.Printf("::%s:: SettleContract(): Before Signing: settleTx: %x \n",os.Args[6][len(os.Args[6])-4:], buft.Bytes())




	if err != nil {
		logging.Errorf("SettleContract SettlementTx err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	mySig, err := nd.SignSettlementTx(c, settleTx, priv)
	if err != nil {
		logging.Errorf("SettleContract SignSettlementTx err %s", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	myBigSig := sig64.SigDecompress(mySig)

	theirSig, err := c.GetTheirSettlementSignature(oracleValue)
	theirBigSig := sig64.SigDecompress(theirSig)

	fmt.Printf("::%s:: SettleContract():theirSig: %x \n",os.Args[6][len(os.Args[6])-4:], theirSig)

	// put the sighash all byte on the end of both signatures
	myBigSig = append(myBigSig, byte(txscript.SigHashAll))
	theirBigSig = append(theirBigSig, byte(txscript.SigHashAll))

	pre, swap, err := lnutil.FundTxScript(c.OurFundMultisigPub, c.TheirFundMultisigPub)
	if err != nil {
		logging.Errorf("SettleContract FundTxScript err %s", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	// swap if needed
	if swap {
		settleTx.TxIn[0].Witness = SpendMultiSigWitStack(pre, theirBigSig, myBigSig)

		fmt.Printf("::%s:: SettleContract(): swap True: settleTx.TxIn[0].Witness Size %d \n", os.Args[6][len(os.Args[6])-4:], settleTx.TxIn[0].Witness.SerializeSize())

	} else {
		settleTx.TxIn[0].Witness = SpendMultiSigWitStack(pre, myBigSig, theirBigSig)

		fmt.Printf("::%s:: SettleContract(): settleTx.TxIn[0].Witness Size %d \n", os.Args[6][len(os.Args[6])-4:], settleTx.TxIn[0].Witness.SerializeSize())

	}



	var buftt bytes.Buffer
	wttt := bufio.NewWriter(&buftt)
	settleTx.Serialize(wttt)
	wttt.Flush()

	fmt.Printf("::%s:: SettleContract(): After Signing: settleTx %x \n",os.Args[6][len(os.Args[6])-4:], buftt.Bytes())


	stxvsize := (settleTx.SerializeSizeStripped() * 3 + settleTx.SerializeSize())/4
	
	fmt.Printf("::%s::SettleContract(): qln/dlc.go: SettleTX size %d \n", os.Args[6][len(os.Args[6])-4:], settleTx.SerializeSize())
	fmt.Printf("::%s::SettleContract(): qln/dlc.go: SettleTX size Stripped %d \n", os.Args[6][len(os.Args[6])-4:], settleTx.SerializeSizeStripped())
	fmt.Printf("::%s::SettleContract(): qln/dlc.go: SettleTX vsize %d \n", os.Args[6][len(os.Args[6])-4:], stxvsize)	


	// Settlement TX should be valid here, so publish it.
	err = wal.DirectSendTx(settleTx)
	if err != nil {
		logging.Errorf("SettleContract DirectSendTx (settle) err %s", err.Error())
		return [32]byte{}, [32]byte{}, err
	}




	//===========================================
	// Claim TX
	//===========================================


	// Here the transaction size is always the same
	// n := 8 + VarIntSerializeSize(uint64(len(msg.TxIn))) +
	// 	VarIntSerializeSize(uint64(len(msg.TxOut)))
	// n = 10
	// Plus Single input 41
	// Plus Single output 31
	// Plus 2 for all wittness transactions
	// Plus Witness Data 151

	// TxSize = 4 + 4 + 1 + 1 + 2 + 151 + 41 + 31 = 235
	// Vsize = ((235 - 151 - 2) * 3 + 235) / 4 = 120,25


	fmt.Printf("::%s::SettleContract(): Claim: d.ValueOurs: %d \n", os.Args[6][len(os.Args[6])-4:], d.ValueOurs)


	if ( d.ValueOurs != 0){


		vsize := int64(121)
		fee := vsize * 80


		// TODO: Claim the contract settlement output back to our wallet - otherwise the peer can claim it after locktime.
		txClaim := wire.NewMsgTx()
		txClaim.Version = 2

		settleOutpoint := wire.OutPoint{Hash: settleTx.TxHash(), Index: 0}
		txClaim.AddTxIn(wire.NewTxIn(&settleOutpoint, nil, nil))

		addr, err := wal.NewAdr()
		txClaim.AddTxOut(wire.NewTxOut(settleTx.TxOut[0].Value-fee, lnutil.DirectWPKHScriptFromPKH(addr)))


		kg.Step[2] = UseContractPayoutBase
		privSpend, _ := wal.GetPriv(kg)

		pubSpend := wal.GetPub(kg)
		privOracle, pubOracle := koblitz.PrivKeyFromBytes(koblitz.S256(), oracleSig[:])
		privContractOutput := lnutil.CombinePrivateKeys(privSpend, privOracle)

		var pubOracleBytes [33]byte
		copy(pubOracleBytes[:], pubOracle.SerializeCompressed())
		var pubSpendBytes [33]byte
		copy(pubSpendBytes[:], pubSpend.SerializeCompressed())

		settleScript := lnutil.DlcCommitScript(c.OurPayoutBase, pubOracleBytes, c.TheirPayoutBase, 5)
		err = nd.SignClaimTx(txClaim, settleTx.TxOut[0].Value, settleScript, privContractOutput, false)

		for txout_idx, settle_txout := range settleTx.TxOut {
			fmt.Printf("::%s:: SettleContract(): settleTx.TxOut[%d].Value: %d \n",os.Args[6][len(os.Args[6])-4:],txout_idx, settle_txout.Value)
		}
		

		if err != nil {
			logging.Errorf("SettleContract SignClaimTx err %s", err.Error())
			return [32]byte{}, [32]byte{}, err
		}

		var buf bytes.Buffer
		wt := bufio.NewWriter(&buf)
		txClaim.Serialize(wt)
		wt.Flush()

		fmt.Printf("::%s:: SettleContract(): qln/dlc.go: txClaim %x \n",os.Args[6][len(os.Args[6])-4:], buf.Bytes())



		//=====================================================================


		n := 8 + VarIntSerializeSize(uint64(len(txClaim.TxIn))) +
		VarIntSerializeSize(uint64(len(txClaim.TxOut)))
	
	
		n_out := 0
		for i1, outtx := range txClaim.TxOut {
	
			n_out += outtx.SerializeSize()
			fmt.Printf("::%s:: i1 : %d \n",os.Args[6][len(os.Args[6])-4:], i1)
	
		}
	
		n_in := 0
		for i2, intx := range txClaim.TxIn {
	
			n_in += intx.SerializeSize()
			fmt.Printf("::%s:: i2 : %d \n",os.Args[6][len(os.Args[6])-4:], i2)
	
		}



		fmt.Printf("::%s:: SettleContract(): qln/dlc.go: txClaim n: %d \n",os.Args[6][len(os.Args[6])-4:], n)			// 10
		fmt.Printf("::%s:: SettleContract(): qln/dlc.go: txClaim n_out: %d \n",os.Args[6][len(os.Args[6])-4:], n_out)	// 31
		fmt.Printf("::%s:: SettleContract(): qln/dlc.go: txClaim n_in: %d \n",os.Args[6][len(os.Args[6])-4:], n_in)		// 41	



		fmt.Printf("::%s:: SettleContract(): txClaim.TxIn[0].Witness Size %d \n", os.Args[6][len(os.Args[6])-4:], txClaim.TxIn[0].Witness.SerializeSize())  // 151


		fmt.Printf("::%s:: SettleContract(): qln/dlc.go: txClaim.SerializeSize() : %d \n",os.Args[6][len(os.Args[6])-4:], txClaim.SerializeSize())    // 235
		fmt.Printf("::%s:: SettleContract(): qln/dlc.go.go: txClaim.SerializeSizeStripped() : %d \n",os.Args[6][len(os.Args[6])-4:], txClaim.SerializeSizeStripped())  // 82
	


		ctxvsize := (txClaim.SerializeSizeStripped() * 3 + txClaim.SerializeSize())/4

		fmt.Printf("::%s::SettleContract(): qln/dlc.goo: ClaimTX vsize %d \n", os.Args[6][len(os.Args[6])-4:], ctxvsize)  // 120. 121 from the blockchain. good.




		//=====================================================================


		// Claim TX should be valid here, so publish it.
		err = wal.DirectSendTx(txClaim)
		if err != nil {
			logging.Errorf("SettleContract DirectSendTx (claim) err %s", err.Error())
			return [32]byte{}, [32]byte{}, err
		}

		c.Status = lnutil.ContractStatusClosed
		err = nd.DlcManager.SaveContract(c)
		if err != nil {
			return [32]byte{}, [32]byte{}, err
		}


		return settleTx.TxHash(), txClaim.TxHash(), nil


	}else{

		return settleTx.TxHash(), [32]byte{}, nil

	}

}
