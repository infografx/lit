import testlib
import test_combinators

def forward(env):
    try:
        lit1 = env.lits[0]
        lit2 = env.lits[1]
        test_combinators.run_close_test(env, lit1, lit2, lit1)
    except BaseException as be:
        raise be    

def reverse(env):
    try:
        lit1 = env.lits[0]
        lit2 = env.lits[1]
        test_combinators.run_close_test(env, lit1, lit2, lit1)
    except BaseException as be:
        raise be    
