#!/bin/bash 
__dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source $__dir/useful_commands.sh
set -e

GASPRICE="0.000000001ulava"
NODE="http://127.0.0.1:3340/1"
STAKE="500000000000ulava"

# Goerli providers
lavad tx pairing stake-provider "GTH1" $STAKE "127.0.0.1:2121,1" 1 $(operator_address) -y --from servicer1 --delegate-limit $STAKE --provider-moniker "dummyMoniker" --gas-adjustment "1.5" --gas "auto" --gas-prices $GASPRICE --node $NODE
wait_next_block
lavad tx pairing stake-provider "GTH1" $STAKE "127.0.0.1:2122,1" 1 $(operator_address) -y --from servicer2 --delegate-limit $STAKE --provider-moniker "dummyMoniker" --gas-adjustment "1.5" --gas "auto" --gas-prices $GASPRICE --node $NODE
wait_next_block
lavad tx pairing stake-provider "GTH1" $STAKE "127.0.0.1:2123,1" 1 $(operator_address) -y --from servicer3 --delegate-limit $STAKE --provider-moniker "dummyMoniker" --gas-adjustment "1.5" --gas "auto" --gas-prices $GASPRICE --node $NODE
wait_next_block
lavad tx pairing stake-provider "GTH1" $STAKE "127.0.0.1:2124,1" 1 $(operator_address) -y --from servicer4 --delegate-limit $STAKE --provider-moniker "dummyMoniker" --gas-adjustment "1.5" --gas "auto" --gas-prices $GASPRICE --node $NODE
wait_next_block
lavad tx pairing stake-provider "GTH1" $STAKE "127.0.0.1:2125,1" 1 $(operator_address) -y --from servicer5 --delegate-limit $STAKE --provider-moniker "dummyMoniker" --gas-adjustment "1.5" --gas "auto" --gas-prices $GASPRICE --node $NODE
wait_next_block
lavad tx subscription buy DefaultPlan $(lavad keys show user1 -a) -y --from user1 --gas-adjustment "1.5" --gas "auto" --gas-prices $GASPRICE

# we need to wait for the next epoch for the stake to take action.
sleep_until_next_epoch