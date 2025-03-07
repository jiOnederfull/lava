package keeper

import (
	"fmt"
	"strconv"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lavanet/lava/utils"
	"github.com/lavanet/lava/x/pairing/types"
)

// SetEpochPayments set a specific epochPayments in the store from its index
func (k Keeper) SetEpochPayments(ctx sdk.Context, epochPayments types.EpochPayments) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix(types.EpochPaymentsKeyPrefix))
	b := k.cdc.MustMarshal(&epochPayments)
	store.Set(types.EpochPaymentsKey(
		epochPayments.Index,
	), b)
}

// GetEpochPayments returns a epochPayments from its index
func (k Keeper) GetEpochPayments(
	ctx sdk.Context,
	index string,
) (val types.EpochPayments, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix(types.EpochPaymentsKeyPrefix))

	b := store.Get(types.EpochPaymentsKey(
		index,
	))
	if b == nil {
		return val, false
	}

	k.cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k EpochPaymentHandler) SetEpochPaymentsCached(ctx sdk.Context, epochPayments types.EpochPayments) {
	b := k.cdc.MustMarshal(&epochPayments)
	k.EpochPaymentsCache.Set(types.EpochPaymentsKey(epochPayments.Index), b)
}

func (k EpochPaymentHandler) GetEpochPaymentsCached(
	ctx sdk.Context,
	index string,
) (val types.EpochPayments, found bool) {
	b := k.EpochPaymentsCache.Get(types.EpochPaymentsKey(index))
	if b == nil {
		return val, false
	}

	k.cdc.MustUnmarshal(b, &val)
	return val, true
}

// RemoveEpochPayments removes a epochPayments from the store
func (k Keeper) RemoveEpochPayments(
	ctx sdk.Context,
	index string,
) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix(types.EpochPaymentsKeyPrefix))
	store.Delete(types.EpochPaymentsKey(
		index,
	))
}

// GetAllEpochPayments returns all epochPayments
func (k Keeper) GetAllEpochPayments(ctx sdk.Context) (list []types.EpochPayments) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix(types.EpochPaymentsKeyPrefix))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.EpochPayments
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}

// Function to remove epochPayments objects from deleted epochs (older than the chain's memory)
func (k Keeper) RemoveOldEpochPayment(ctx sdk.Context) {
	for _, epoch := range k.epochStorageKeeper.GetDeletedEpochs(ctx) {
		k.RemoveAllEpochPaymentsForBlockAppendAdjustments(ctx, epoch)
	}
}

// Function to get the epochPayments object from a specific epoch. Note that it also returns the epochPayments object's key which is the epoch in hex representation (base 16)
func (k Keeper) GetEpochPaymentsFromBlock(ctx sdk.Context, epoch uint64) (epochPayment types.EpochPayments, found bool, key string) {
	key = epochPaymentKey(epoch)
	epochPayment, found = k.GetEpochPayments(ctx, key)
	return
}

func epochPaymentKey(epoch uint64) string {
	return strconv.FormatUint(epoch, 16)
}

type EpochPaymentHandler struct {
	Keeper
	EpochPaymentsCache                      *cachekv.Store
	ProviderPaymentStorageCache             *cachekv.Store
	UniquePaymentStorageClientProviderCache *cachekv.Store
}

func (k Keeper) NewEpochPaymentHandler(ctx sdk.Context) EpochPaymentHandler {
	return EpochPaymentHandler{
		Keeper:                                  k,
		EpochPaymentsCache:                      cachekv.NewStore(prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix(types.EpochPaymentsKeyPrefix))),
		ProviderPaymentStorageCache:             cachekv.NewStore(prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix(types.ProviderPaymentStorageKeyPrefix))),
		UniquePaymentStorageClientProviderCache: cachekv.NewStore(prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix(types.UniquePaymentStorageClientProviderKeyPrefix))),
	}
}

func (k EpochPaymentHandler) Flush() {
	k.EpochPaymentsCache.Write()
	k.ProviderPaymentStorageCache.Write()
	k.UniquePaymentStorageClientProviderCache.Write()
}

// Function to add an epoch payment to the epochPayments object
func (k EpochPaymentHandler) AddEpochPayment(ctx sdk.Context, chainID string, epoch uint64, projectID string, providerAddress sdk.AccAddress, usedCU uint64, uniqueIdentifier string) uint64 {
	if epoch < k.epochStorageKeeper.GetEarliestEpochStart(ctx) {
		return 0
	}

	// add a uniquePaymentStorageClientProvider object (the object that represent the actual payment) to this epoch's providerPaymentPayment object
	userPaymentProviderStorage, usedCUProviderTotal := k.AddProviderPaymentInEpoch(ctx, chainID, epoch, projectID, providerAddress, usedCU, uniqueIdentifier)

	// get this epoch's epochPayments object
	key := epochPaymentKey(epoch)
	epochPayments, found := k.GetEpochPaymentsCached(ctx, key)
	if !found {
		// this epoch doesn't have a epochPayments object, create one with the providerPaymentStorage object from before
		epochPayments = types.EpochPayments{Index: key, ProviderPaymentStorageKeys: []string{userPaymentProviderStorage.GetIndex()}}
	} else {
		// this epoch has a epochPayments object -> make sure this payment is not already in this object
		// TODO: improve - have it sorted and binary search, store indexes map for the current epoch providers stake and just lookup at the provider index (and turn it on) - assumes most providers will have payments
		providerPaymentStorageKeyFound := false
		for _, providerPaymentStorageKey := range epochPayments.GetProviderPaymentStorageKeys() {
			if providerPaymentStorageKey == userPaymentProviderStorage.GetIndex() {
				providerPaymentStorageKeyFound = true
				break
			}
		}

		// this epoch's epochPayments object doesn't contain this providerPaymentStorage key -> append the new key
		if !providerPaymentStorageKeyFound {
			epochPayments.ProviderPaymentStorageKeys = append(epochPayments.ProviderPaymentStorageKeys, userPaymentProviderStorage.GetIndex())
		}
	}

	// update the epochPayments object
	k.SetEpochPaymentsCached(ctx, epochPayments)

	return usedCUProviderTotal
}

// Function to remove all epochPayments objects from a specific epoch
func (k Keeper) RemoveAllEpochPaymentsForBlockAppendAdjustments(ctx sdk.Context, blockForDelete uint64) {
	// get the epochPayments object of blockForDelete
	epochPayments, found, key := k.GetEpochPaymentsFromBlock(ctx, blockForDelete)
	if !found {
		return
	}

	// TODO: update Qos in providerQosFS. new consumers (cluster.subUsage = 0) get default QoS (what is default?)
	consumerUsage := map[string]uint64{}
	type couplingConsumerProvider struct {
		consumer string
		provider string
	}
	// we are keeping the iteration keys to keep determinism when going over the map
	iterationOrder := []couplingConsumerProvider{}
	couplingUsage := map[couplingConsumerProvider]uint64{}
	// go over the epochPayments object's providerPaymentStorageKeys
	userPaymentsStorageKeys := epochPayments.GetProviderPaymentStorageKeys()
	for _, userPaymentStorageKey := range userPaymentsStorageKeys {
		// get the providerPaymentStorage object
		providerPaymentStorage, found := k.GetProviderPaymentStorage(ctx, userPaymentStorageKey)
		if !found {
			continue
		}

		// go over the providerPaymentStorage object's uniquePaymentStorageClientProviderKeys
		uniquePaymentStoragesCliProKeys := providerPaymentStorage.GetUniquePaymentStorageClientProviderKeys()
		for _, uniquePaymentStorageKey := range uniquePaymentStoragesCliProKeys {
			// get the uniquePaymentStorageClientProvider object
			uniquePaymentStorage, found := k.GetUniquePaymentStorageClientProvider(ctx, uniquePaymentStorageKey)
			if !found {
				continue
			}

			// validate its an old entry, for sanity
			if uniquePaymentStorage.Block > blockForDelete {
				// this should not happen; to avoid panic we simply skip this one (thus
				// freeze the situation so it can be investigated and orderly resolved).
				utils.LavaFormatError("critical: failed to delete epoch payment",
					fmt.Errorf("payment block greater than block for delete"),
					utils.Attribute{Key: "paymentBlock", Value: uniquePaymentStorage.Block},
					utils.Attribute{Key: "deleteBlock", Value: blockForDelete},
				)
				continue
			}

			// delete the uniquePaymentStorageClientProvider object
			k.RemoveUniquePaymentStorageClientProvider(ctx, uniquePaymentStorage.Index)
			consumer := k.GetConsumerFromUniquePayment(uniquePaymentStorageKey)

			provider, err := k.GetProviderFromProviderPaymentStorage(&providerPaymentStorage)
			if err != nil {
				utils.LavaFormatError("failed getting provider from payment storage", err)
				continue
			}
			coupling := couplingConsumerProvider{consumer: consumer, provider: provider}
			if _, ok := couplingUsage[coupling]; !ok {
				// only add it if it doesn't exist
				iterationOrder = append(iterationOrder, coupling)
			}
			consumerUsage[consumer] += uniquePaymentStorage.UsedCU
			couplingUsage[coupling] += uniquePaymentStorage.UsedCU
		}

		// after we're done deleting the uniquePaymentStorageClientProvider objects, delete the providerPaymentStorage object
		k.RemoveProviderPaymentStorage(ctx, providerPaymentStorage.Index)
	}
	for _, coupling := range iterationOrder {
		k.subscriptionKeeper.AppendAdjustment(ctx, coupling.consumer, coupling.provider, consumerUsage[coupling.consumer], couplingUsage[coupling])
	}
	// after we're done deleting the providerPaymentStorage objects, delete the epochPayments object
	k.RemoveEpochPayments(ctx, key)
}
