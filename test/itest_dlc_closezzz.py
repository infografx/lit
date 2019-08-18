import testlib
import time

import pprint
pp = pprint.PrettyPrinter(indent=4)

fee = 20
initialsend = 200000
capacity = 1000000
pushsend = 250000

def run_close_test(env, initiator, target, closer):
    bc = env.bitcoind

    # Connect the nodes.
    initiator.connect_to_peer(target)


    #-------------------------------------------------------------------------

    print("ADDRESSES BEFORE SEND TO ADDRESS")
    print("LIT1 Addresses")
    print(pp.pprint(initiator.rpc.GetAddresses()))

    print("LIT2 Addresses")
    print(pp.pprint(target.rpc.GetAddresses()))

    print("bitcoind Addresses")
    print(pp.pprint(bc.rpc.listaddressgroupings()))

    bals1 = initiator.get_balance_info()  
    print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
    bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
    print('  = sum ', bal1sum)

    bals2 = target.get_balance_info()
    print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
    bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
    print('  = sum ', bal2sum)   


    #-------------------------------------------------------------------------


    # First figure out where we should send the money.
    addr1 = initiator.make_new_addr()
    print('Got initiator address:', addr1)

    # Send a bitcoin.
    bc.rpc.sendtoaddress(addr1, 1)
    env.generate_block()


    #-------------------------------------------------------------------------


    print("ADDRESSES AFTER SEND TO ADDRESS")
    print("LIT1 Addresses")
    print(pp.pprint(initiator.rpc.GetAddresses()))

    print("LIT2 Addresses")
    print(pp.pprint(target.rpc.GetAddresses()))

    print("bitcoind Addresses")
    print(pp.pprint(bc.rpc.listaddressgroupings()))

    bals1 = initiator.get_balance_info()  
    print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
    bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
    print('  = sum ', bal1sum)

    bals2 = target.get_balance_info()
    print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
    bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
    print('  = sum ', bal2sum)  


    #-------------------------------------------------------------------------    


    # Log it to make sure we got it.
    bal1 = initiator.get_balance_info()['TxoTotal']
    print('initial initiator balance:', bal1)

    # Set the fee so we know what's going on.
    initiator.rpc.SetFee(Fee=fee, CoinType=testlib.REGTEST_COINTYPE)
    target.rpc.SetFee(Fee=fee, CoinType=testlib.REGTEST_COINTYPE)

    # Now actually do the funding.
    cid = initiator.open_channel(target, capacity, initialsend)
    print('Created channel:', cid)

    # Now we confirm the block.
    env.generate_block()


    #-------------------------------------------------------------------------


    print("ADDRESSES AFTER OPEN CHANNEL")
    print("LIT1 Addresses")
    print(pp.pprint(initiator.rpc.GetAddresses()))

    print("LIT2 Addresses")
    print(pp.pprint(target.rpc.GetAddresses()))

    print("bitcoind Addresses")
    print(pp.pprint(bc.rpc.listaddressgroupings()))

    bals1 = initiator.get_balance_info()  
    print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
    bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
    print('  = sum ', bal1sum)

    bals2 = target.get_balance_info()
    print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
    bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
    print('  = sum ', bal2sum)  


    #-------------------------------------------------------------------------
    time.sleep(2)

    print("==========================================================")
    print('Now closing...')
    print("==========================================================")


    # Now close the channel.
    
    res = closer.rpc.CloseChannel(ChanIdx=cid)
    print('Status:', res['Status'])
    env.generate_block()
    time.sleep(1)
    env.generate_block()
    time.sleep(1)
    env.generate_block()
    time.sleep(5)

    # Check balances.
    bals = initiator.get_balance_info()
    fbal = bals['TxoTotal']
    print('final balance:', fbal)
    expected = bal1 - initialsend - 3560
    print('expected:', expected)
    print('diff:', expected - fbal)


    print("ADDRESSES AFTER CLOSE CHANNEL")
    print("LIT1 Addresses")
    print(pp.pprint(initiator.rpc.GetAddresses()))

    print("LIT2 Addresses")
    print(pp.pprint(target.rpc.GetAddresses()))

    print("bitcoind Addresses")
    print(pp.pprint(bc.rpc.listaddressgroupings()))

    bals1 = initiator.get_balance_info()  
    print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
    bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
    print('  = sum ', bal1sum)

    bals2 = target.get_balance_info()
    print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
    bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
    print('  = sum ', bal2sum)  


    #=================================================================
    time.sleep(2)

    print("==========================================================")
    print('Print Blockchain Info')
    print("==========================================================")


    

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

    #assert bals['ChanTotal'] == 0, "channel balance isn't zero!"



def forward(env):
    lit1 = env.lits[0]
    lit2 = env.lits[1]

    lit1.resync()
    lit2.resync()

    run_close_test(env, lit1, lit2, lit1)

def reverse(env):
    lit1 = env.lits[0]
    lit2 = env.lits[1]
    run_close_test(env, lit1, lit2, lit2)
