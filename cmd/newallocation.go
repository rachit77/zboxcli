package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/0chain/gosdk/zboxcore/blockchain"

	"github.com/spf13/pflag"

	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/gosdk/zcncore"
	"github.com/0chain/zboxcli/util"
	"github.com/spf13/cobra"
)

var (
	datashards, parityshards *int
	size                     *int64
	allocationFileName       *string
)

func getPriceRange(val string) (pr sdk.PriceRange, err error) {
	var ss = strings.Split(val, "-")
	if len(ss) != 2 {
		err = fmt.Errorf("invalid price range format: %q", val)
		return
	}
	var minf, maxf float64
	if minf, err = strconv.ParseFloat(ss[0], 64); err != nil {
		return
	}
	if maxf, err = strconv.ParseFloat(ss[1], 64); err != nil {
		return
	}
	pr.Min = zcncore.ConvertToValue(minf)
	pr.Max = zcncore.ConvertToValue(maxf)
	return
}

// newallocationCmd represents the new allocation command
var newallocationCmd = &cobra.Command{
	Use:   "newallocation",
	Short: "Creates a new allocation",
	Long:  `Creates a new allocation`,
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		var flags = cmd.Flags()
		costOnly, _ := cmd.Flags().GetBool("cost")

		if flags.Changed("free_storage") {
			if costOnly {
				log.Print("Cost for the given allocation: ", 0)
				return
			}
			lock, freeStorageMarker := processFreeStorageFlags(flags)
			if lock < 0 {
				log.Fatal("Only positive values are allowed for --lock")
			}

			allocationID, _, err := sdk.CreateFreeAllocation(freeStorageMarker, lock)
			if err != nil {
				log.Fatal("Error creating free allocation: ", err)
			}
			log.Print("Allocation created: ", allocationID)
			storeAllocation(allocationID)
			return
		}

		if datashards == nil || parityshards == nil || size == nil {
			log.Fatal("Invalid allocation parameters.")
		}

		var (
			lock uint64 // lock with given number of tokens
			err  error  //
		)

		if !costOnly {
			if !flags.Changed("lock") {
				log.Fatal("missing required 'lock' argument")
			}
		}

		convertFromUSD, _ := cmd.Flags().GetBool("usd")

		var lockf float64
		if lockf, err = flags.GetFloat64("lock"); err != nil {
			log.Fatal("error: invalid 'lock' value:", err)
		}

		if convertFromUSD {
			lockf, err = zcncore.ConvertUSDToToken(lockf)
			if err != nil {
				log.Fatal("error: failed to convert to USD : ", err)
			}
		}
		lock = zcncore.ConvertToValue(lockf)

		const maxPrice = math.MaxUint64 / 100
		var (
			readPrice  = sdk.PriceRange{Min: 0, Max: maxPrice}
			writePrice = sdk.PriceRange{Min: 0, Max: maxPrice}
		)

		if flags.Changed("read_price") {
			rps, err := flags.GetString("read_price")
			if err != nil {
				log.Fatal("invalid read_price value: ", err)
			}
			pr, err := getPriceRange(rps)
			if err != nil {
				log.Fatal("invalid read_price value: ", err)
			}
			readPrice = pr
		} else {
			readPrice, err = sdk.GetReadPriceRange()
			if err != nil {
				log.Fatal("invalid read_price value: ", err)
			}
		}

		if flags.Changed("write_price") {
			wps, err := flags.GetString("write_price")
			if err != nil {
				log.Fatal("invalid write_price value: ", err)
			}
			pr, err := getPriceRange(wps)
			if err != nil {
				log.Fatal("invalid write_price value: ", err)
			}
			writePrice = pr
		} else {
			writePrice, err = sdk.GetWritePriceRange()
			if err != nil {
				log.Fatal("invalid write_price value: ", err)
			}
		}

		var expire time.Duration
		if expire, err = flags.GetDuration("expire"); err != nil {
			log.Fatal("invalid 'expire' flag: ", err)
		}

		var expireAt = time.Now().Add(expire).Unix()

		if costOnly {
			minCost, err := sdk.GetAllocationMinLock(*datashards, *parityshards, *size, expire.Milliseconds(), readPrice, writePrice)
			if err != nil {
				log.Fatal("Error fetching cost: ", err)
			}
			log.Print("Cost for the given allocation: ", zcncore.ConvertToToken(minCost), " ZCN")

			return
		}

		var owner string
		if flags.Changed("owner") {
			if owner, err = flags.GetString("owner"); err != nil {
				log.Fatal("invalid owner value: ", err)
			}
		}

		thirdPartyExtendable, _ := flags.GetBool("third_party_extendable")

		// Read the file options flags
		var fileOptionParams sdk.FileOptionsParameters
		if flags.Changed("forbid_upload") {
			forbidUpload, err := flags.GetBool("forbid_upload")
			if err != nil {
				log.Fatal("invalid forbid_upload: ", err)
			}
			fileOptionParams.ForbidUpload.Changed = true
			fileOptionParams.ForbidUpload.Value = forbidUpload
		}
		if flags.Changed("forbid_delete") {
			forbidDelete, err := flags.GetBool("forbid_delete")
			if err != nil {
				log.Fatal("invalid forbid_upload: ", err)
			}
			fileOptionParams.ForbidDelete.Changed = true
			fileOptionParams.ForbidDelete.Value = forbidDelete
		}
		if flags.Changed("forbid_update") {
			forbidUpdate, err := flags.GetBool("forbid_update")
			if err != nil {
				log.Fatal("invalid forbid_upload: ", err)
			}
			fileOptionParams.ForbidUpdate.Changed = true
			fileOptionParams.ForbidUpdate.Value = forbidUpdate
		}
		if flags.Changed("forbid_move") {
			forbidMove, err := flags.GetBool("forbid_move")
			if err != nil {
				log.Fatal("invalid forbid_upload: ", err)
			}
			fileOptionParams.ForbidMove.Changed = true
			fileOptionParams.ForbidMove.Value = forbidMove
		}
		if flags.Changed("forbid_copy") {
			forbidCopy, err := flags.GetBool("forbid_copy")
			if err != nil {
				log.Fatal("invalid forbid_upload: ", err)
			}
			fileOptionParams.ForbidCopy.Changed = true
			fileOptionParams.ForbidCopy.Value = forbidCopy
		}
		if flags.Changed("forbid_rename") {
			forbidRename, err := flags.GetBool("forbid_rename")
			if err != nil {
				log.Fatal("invalid forbid_upload: ", err)
			}
			fileOptionParams.ForbidRename.Changed = true
			fileOptionParams.ForbidRename.Value = forbidRename
		}

		var allocationID string
		if len(owner) == 0 {
			allocationID, _, _, err = sdk.CreateAllocation(*datashards, *parityshards,
				*size, expireAt, readPrice, writePrice, lock, thirdPartyExtendable, &fileOptionParams)
			if err != nil {
				log.Fatal("Error creating allocation: ", err)
			}
		} else {
			var ownerPublicKey string
			if flags.Changed("owner") {
				if ownerPublicKey, err = flags.GetString("owner_public_key"); err != nil {
					log.Fatal("invalid owner public key: ", err)
				}
				if len(ownerPublicKey) == 0 {
					log.Fatal("must provide owner public key, when creating an allocation for another")
				}
			}

			allocationID, _, _, err = sdk.CreateAllocationForOwner(owner, ownerPublicKey, *datashards, *parityshards,
				*size, expireAt, readPrice, writePrice, lock, blockchain.GetPreferredBlobbers(), thirdPartyExtendable, &fileOptionParams)
			if err != nil {
				log.Fatal("Error creating allocation: ", err)
			}
		}
		log.Print("Allocation created: ", allocationID)
		storeAllocation(allocationID)

	},
}

func processFreeStorageFlags(flags *pflag.FlagSet) (uint64, string) {
	if flags.Changed("read_price") {
		log.Fatal("free storage, read_price is predefined")
	}
	if flags.Changed("write_price") {
		log.Fatal("free storage, write_price is predefined")
	}

	filename, err := flags.GetString("free_storage")
	if err != nil {
		log.Fatal("invalid free)value: ", err)
	}
	freeStorageMarker, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal("cannot read in free_storage file", err)
	}
	var marker struct {
		FreeTokens float64 `json:"free_tokens"`
	}
	err = json.Unmarshal(freeStorageMarker, &marker)
	if err != nil {
		log.Fatal("unmarshalling marker", err)
	}
	return zcncore.ConvertToValue(marker.FreeTokens), string(freeStorageMarker)
}

func init() {
	rootCmd.AddCommand(newallocationCmd)
	datashards = newallocationCmd.PersistentFlags().Int("data", 2, "the number of blobbers to be used as data shards")
	parityshards = newallocationCmd.PersistentFlags().Int("parity", 2, "the number of blobber to be used as parity shards")
	size = newallocationCmd.PersistentFlags().Int64("size", 2147483648, "the size of the allocation")
	allocationFileName = newallocationCmd.PersistentFlags().String("allocationFileName", "allocation.txt", "name of the file in configDir to store the generated allocationID")
	newallocationCmd.PersistentFlags().
		Float64("lock", 0.0,
			"lock write pool with given number of tokens, required")
	newallocationCmd.PersistentFlags().
		String("read_price", "",
			"select blobbers by provided read price range, use form 0.5-1.5, default is [0; inf)")
	newallocationCmd.PersistentFlags().
		String("write_price", "",
			"select blobbers by provided write price range, use form 1.5-2.5, default is [0; inf)")
	newallocationCmd.PersistentFlags().
		Duration("expire", 720*time.Hour, "duration to allocation expiration")

	newallocationCmd.Flags().
		Bool("usd", false,
			"pass this option to give token value in USD")
	newallocationCmd.Flags().
		Bool("cost", false,
			"pass this option to only get the min lock demand")
	newallocationCmd.Flags().
		String("free_storage", "",
			"json file containing marker for free storage")
	newallocationCmd.Flags().String("owner", "",
		"create an allocation with someone else as owner")
	newallocationCmd.Flags().String("owner_public_key", "",
		"public key of owner, user when creating an allocation for somone else")

	newallocationCmd.Flags().String("name", "", "allocation name")

	newallocationCmd.Flags().Bool("third_party_extendable", false, "specify if the allocation can be extended by users other than the owner")
	newallocationCmd.Flags().Bool("forbid_upload", false, "specify if users cannot upload to this allocation")
	newallocationCmd.Flags().Bool("forbid_delete", false, "specify if the users cannot delete objects from this allocation")
	newallocationCmd.Flags().Bool("forbid_update", false, "specify if the users cannot update objects in this allocation")
	newallocationCmd.Flags().Bool("forbid_move", false, "specify if the users cannot move objects from this allocation")
	newallocationCmd.Flags().Bool("forbid_copy", false, "specify if the users cannot copy object from this allocation")
	newallocationCmd.Flags().Bool("forbid_rename", false, "specify if the users cannot rename objects in this allocation")
}

func storeAllocation(allocationID string) {

	allocFilePath := util.GetConfigDir() + string(os.PathSeparator) + *allocationFileName

	file, err := os.Create(allocFilePath)
	if err != nil {
		PrintError(err.Error())
		os.Exit(1)
	}
	defer file.Close()
	//Only one allocation ID per file.
	fmt.Fprint(file, allocationID)

}
