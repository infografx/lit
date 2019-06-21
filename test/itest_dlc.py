import testlib

import time, datetime
import json

import pprint

import requests # pip3 install requests

import codecs


def run_test(env):

    try:
        bc = env.bitcoind

        #------------
        # Create oracles
        #------------

        env.new_oracle(1, 11, 20) # publishing interval is 1 second.
        env.new_oracle(1, 11, 20)

        oracle1 = env.oracles[0]
        oracle2 = env.oracles[1]

        time.sleep(2)

        #------------
        # Create lits
        #------------

        lit1 = env.lits[0]
        lit2 = env.lits[1]


        pp = pprint.PrettyPrinter(indent=4)


        #------------------------------------------
        print("ADDRESSES BEFORE SEND TO ADDRESS")
        print("LIT1 Addresses")
        print(pp.pprint(lit1.rpc.GetAddresses()))

        print("LIT2 Addresses")
        print(pp.pprint(lit2.rpc.GetAddresses()))

        print("bitcoind Addresses")
        print(pp.pprint(bc.rpc.listaddressgroupings()))
        #------------------------------------------ 


        lit1.connect_to_peer(lit2)
        print("---------------")
        print('Connecting lit1:', lit1.lnid, 'to lit2:', lit2.lnid)

        addr1 = lit1.make_new_addr()
        txid1 = bc.rpc.sendtoaddress(addr1, 1)

        print("TXID1: " + str(txid1))

        time.sleep(5)

        addr2 = lit2.make_new_addr()
        txid2 = bc.rpc.sendtoaddress(addr2, 1)
        print("TXID2: " + str(txid2))

        time.sleep(5)

        env.generate_block()
        time.sleep(5)

        print("Funding")
        bals1 = lit1.get_balance_info()  
        print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
        bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
        print('  = sum ', bal1sum)

        bals2 = lit2.get_balance_info()
        print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
        bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
        print('  = sum ', bal2sum) 


        #------------------------------------------
        print("ADDRESSES AFTER SEND TO ADDRESS")
        print("LIT1 Addresses")
        print(pp.pprint(lit1.rpc.GetAddresses()))

        print("LIT2 Addresses")
        print(pp.pprint(lit2.rpc.GetAddresses()))

        print("bitcoind Addresses")
        print(pp.pprint(bc.rpc.listaddressgroupings()))
        #------------------------------------------          


        # #------------
        # # Add oracles
        # #------------

        res = lit1.rpc.ListOracles()
        assert len(res) != 0, "Initial lis of oracles must be empty"
        
        oracle1_pubkey = json.loads(oracle1.get_pubkey())
        assert len(oracle1_pubkey["A"]) == 66, "Wrong oracle1 pub key"
        
        # oracle2_pubkey = json.loads(oracle2.get_pubkey())
        # assert len(oracle2_pubkey["A"]) == 66, "Wrong oracle2 pub key"

        oracle_res1 = lit1.rpc.AddOracle(Key=oracle1_pubkey["A"], Name="oracle1")
        assert oracle_res1["Oracle"]["Idx"] == 1, "AddOracle does not works"

        res = lit1.rpc.ListOracles(ListOraclesArgs={})
        assert len(res["Oracles"]) == 1, "ListOracles 1 does not works"


        # oracle_res2 = lit1.rpc.ImportOracle(Url="http://localhost:" + oracle2.httpport)
        # assert oracle_res2["Oracle"]["Idx"] == 2, "ImportOracle does not works"

        # res = lit1.rpc.ListOracles(ListOraclesArgs={})
        # assert len(res["Oracles"]) == 2, "ListOracles 2 does not works"

        lit2.rpc.AddOracle(Key=oracle1_pubkey["A"], Name="oracle1")


        # #------------
        # # Now we have to create a contract in the lit1 node.
        # #------------

        contract = lit1.rpc.NewContract()

        res = lit1.rpc.ListContracts()
        assert len(res["Contracts"]) == 1, "ListContracts does not works"


        res = lit1.rpc.GetContract(Idx=1)
        assert res["Contract"]["Idx"] == 1, "GetContract does not works"
                

        res = lit1.rpc.SetContractOracle(CIdx=contract["Contract"]["Idx"], OIdx=oracle_res1["Oracle"]["Idx"])
        assert res["Success"], "SetContractOracle does not works"

        datasources = json.loads(oracle1.get_datasources())

        # # Since the oracle publishes data every 1 second (we set this time above), 
        # # we increase the time for a point by 3 seconds.

        settlement_time = int(time.time()) + 3

        # dlc contract settime 1 1552080600
        lit1.rpc.SetContractSettlementTime(CIdx=contract["Contract"]["Idx"], Time=settlement_time)

        res = lit1.rpc.ListContracts()
        assert res["Contracts"][contract["Contract"]["Idx"] - 1]["OracleTimestamp"] == settlement_time, "SetContractSettlementTime does not match settlement_time"

        rpoint1 = oracle1.get_rpoint(datasources[0]["id"], settlement_time)

        decode_hex = codecs.getdecoder("hex_codec")
        b_RPoint = decode_hex(json.loads(rpoint1)['R'])[0]
        RPoint = [elem for elem in b_RPoint]

        res = lit1.rpc.SetContractRPoint(CIdx=contract["Contract"]["Idx"], RPoint=RPoint)
        assert res["Success"], "SetContractRpoint does not works"

        lit1.rpc.SetContractCoinType(CIdx=contract["Contract"]["Idx"], CoinType = 257)
        res = lit1.rpc.GetContract(Idx=contract["Contract"]["Idx"])
        assert res["Contract"]["CoinType"] == 257, "SetContractCoinType does not works"

        ourFundingAmount = 10000000
        theirFundingAmount = 10000000

        lit1.rpc.SetContractFunding(CIdx=contract["Contract"]["Idx"], OurAmount=ourFundingAmount, TheirAmount=theirFundingAmount)
        res = lit1.rpc.GetContract(Idx=contract["Contract"]["Idx"])
        assert res["Contract"]["OurFundingAmount"] == ourFundingAmount, "SetContractFunding does not works"
        assert res["Contract"]["TheirFundingAmount"] == theirFundingAmount, "SetContractFunding does not works"

        valueFullyOurs=20
        valueFullyTheirs=10

        res = lit1.rpc.SetContractDivision(CIdx=contract["Contract"]["Idx"], ValueFullyOurs=valueFullyOurs, ValueFullyTheirs=valueFullyTheirs)
        assert res["Success"], "SetContractDivision does not works"

        time.sleep(5)
        print("Before OfferContract")

        res = lit1.rpc.ListConnections()
        print(res)


        print("=====CONTRACT IN LIT1 AFTER SetContractDivision=====")
        res = lit1.rpc.ListContracts()
        print(pp.pprint(res))
        print("=====CONTRACT IN LIT1=====")



        res = lit1.rpc.OfferContract(CIdx=contract["Contract"]["Idx"], PeerIdx=lit1.get_peer_id(lit2))
        assert res["Success"], "OfferContract does not works"
        time.sleep(5)
       

        print("After OfferContract")
        print("Before ContractRespond")

        res = lit2.rpc.ContractRespond(AcceptOrDecline=True, CIdx=1)
        assert res["Success"], "ContractRespond on lit2 does not works"

        time.sleep(5)


        print("After ContractRespond")


        print("=====START CONTRACT N1=====")
        res = lit1.rpc.ListContracts()
        print(pp.pprint(res))
        print("=====END CONTRACT N1=====")

        print("=====START CONTRACT N2=====")
        res = lit2.rpc.ListContracts()
        print(pp.pprint(res))
        print("=====END CONTRACT N2=====") 


        #------------------------------------------
        print("ADDRESSES AFTER CONTRACT RESPOND")
        print("LIT1 Addresses")
        print(lit1.rpc.GetAddresses())

        print("LIT2 Addresses")
        print(lit2.rpc.GetAddresses())

        print("bitcoind Addresses")
        print(bc.rpc.listaddressgroupings())
        #------------------------------------------  


        print("Before Generate Block")

        env.generate_block()
        time.sleep(5)

        print("After Generate Block")

        print("Accept")
        bals1 = lit1.get_balance_info()  
        print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
        bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
        print('  = sum ', bal1sum)

        bals2 = lit2.get_balance_info()
        print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
        bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
        print('  = sum ', bal2sum)   


        oracle1_val = ""
        oracle1_sig = ""

        i = 0
        while True:
            res = oracle1.get_publication(json.loads(rpoint1)['R'])
            time.sleep(5)
            i += 1
            if i>4:
                assert False, "Error: Oracle does not publish data"
            
            try:
                oracle1_val = json.loads(res)["value"]
                oracle1_sig = json.loads(res)["signature"]
                break
            except BaseException as e:
                print(e)
                next

        print("ORACLE VALUE:", oracle1_val, "; oracle signature:", oracle1_sig)   


        b_OracleSig = decode_hex(oracle1_sig)[0]
        OracleSig = [elem for elem in b_OracleSig]


        print("Before SettleContract")
        time.sleep(5)


        res = lit1.rpc.SettleContract(CIdx=contract["Contract"]["Idx"], OracleValue=oracle1_val, OracleSig=OracleSig)
        assert res["Success"], "SettleContract does not works."

        time.sleep(10)

        print('After SettleContract:')
        print(res)

        # bc.rpc.generate(2)

        # time.sleep(2)

        # bc.rpc.generate(2)

        # time.sleep(2)

        # bc.rpc.generate(2)



        try:
            env.generate_block(1)
            time.sleep(2)
            env.generate_block(10)
            time.sleep(2)
            env.generate_block(1)
        except BaseException as be:
            print(be)    

        time.sleep(10)

        # # And we get an error here:
        # # 2019/04/24 14:33:27.516819 [ERROR] Write message error: write tcp4 127.0.0.1:51938->127.0.0.1:11000: use of closed network connection
        # # Error: lits aren't syncing to bitcoind

        # # time.sleep(1)

        # # print("--------end")

        #------------------------------------------


        best_block_hash = bc.rpc.getbestblockhash()
        bb = bc.rpc.getblock(best_block_hash)
        print(bb)
        print("bb['height']: " + str(bb['height']))

        print("Balance from RPC: " + str(bc.rpc.getbalance()))

        # batch support : print timestamps of blocks 0 to 99 in 2 RPC round-trips:
        commands = [ [ "getblockhash", height] for height in range(bb['height'] + 1) ]
        block_hashes = bc.rpc.batch_(commands)
        blocks = bc.rpc.batch_([ [ "getblock", h ] for h in block_hashes ])
        block_times = [ block["time"] for block in blocks ]
        print(block_times)

        print('--------------------')

        for b in blocks:
            print("--------BLOCK--------")
            print(b)
            tx = b["tx"]
            #print(tx)
            try:

                for i in range(len(tx)):
                    print("--------TRANSACTION--------")
                    rtx = bc.rpc.getrawtransaction(tx[i])
                    print(rtx)
                    decoded = bc.rpc.decoderawtransaction(rtx)
                    pp.pprint(decoded)
            except BaseException as be:
                print(be)
            # print(type(rtx))
            print('--------')


        #------------------------------------------
        #------------------------------------------
        print("AFter Settle")
        bals1 = lit1.get_balance_info()  
        print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
        bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
        print('  = sum ', bal1sum)
        print(bals1)

        bals2 = lit2.get_balance_info()
        print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
        bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
        print('  = sum ', bal2sum)
        print(bals2)


        #------------------------------------------
        print("ADDRESSES AFTER SETTLE")
        print("LIT1 Addresses")
        print(pp.pprint(lit1.rpc.GetAddresses()))

        print("LIT2 Addresses")
        print(pp.pprint(lit2.rpc.GetAddresses()))

        print("bitcoind Addresses")
        print(pp.pprint(bc.rpc.listaddressgroupings()))
        #------------------------------------------   
        
        

    except BaseException as be:
        raise be
    






