package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/0chain/gosdk/constants"
	"github.com/0chain/gosdk/zboxcore/logger"
	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/gosdk/zboxcore/zboxutil"
	"github.com/0chain/zboxcli/util"
	"github.com/spf13/cobra"
)

func printTable(files []sdk.FileDiff) {
	header := []string{"Operation", "Path"}
	data := make([][]string, len(files))
	for idx, child := range files {
		data[idx] = []string{child.Op, child.Path}
	}
	util.WriteTable(os.Stdout, header, []string{}, data)
	fmt.Println("")
}

func saveCache(allocationObj *sdk.Allocation, path string, exclPath []string) {
	if len(path) > 0 {
		err := allocationObj.SaveRemoteSnapshot(path, exclPath)
		if err != nil {
			PrintError("Failed to save local cache.", err)
			os.Exit(1)
		}
		fmt.Println("Local cache saved.")
	}
}

func filterOperations(lDiff []sdk.FileDiff) (filterDiff []sdk.FileDiff, exclPath []string) {
	for _, f := range lDiff {
		if f.Op == sdk.Update || f.Op == sdk.Upload {
			filterDiff = append(filterDiff, f)
		} else {
			exclPath = append(exclPath, f.Path)
		}
	}
	return
}

func startMultiUploadUpdate(allocationObj *sdk.Allocation, argsSlice []chunkedUploadArgs) error {
	totalOperations := len(argsSlice)
	if totalOperations == 0 {
		return nil
	}
	operationRequests := make([]sdk.OperationRequest, totalOperations)
	wg := &sync.WaitGroup{}
	for idx, args := range argsSlice {
		statusBar := &StatusBar{wg: wg}
		wg.Add(1)
		fileReader, err := os.Open(args.localPath)
		if err != nil {
			return err
		}
		defer fileReader.Close()

		fileInfo, err := fileReader.Stat()
		if err != nil {
			return err
		}

		mimeType, err := zboxutil.GetFileContentType(fileReader)
		if err != nil {
			return err
		}

		remotePath, fileName, err := fullPathAndFileNameForUpload(args.localPath, args.remotePath)
		if err != nil {
			return err
		}

		fileMeta := sdk.FileMeta{
			Path:       args.localPath,
			ActualSize: fileInfo.Size(),
			MimeType:   mimeType,
			RemoteName: fileName,
			RemotePath: remotePath,
		}
		options := []sdk.ChunkedUploadOption{
			sdk.WithThumbnailFile(args.thumbnailPath),
			sdk.WithEncrypt(args.encrypt),
			sdk.WithStatusCallback(statusBar),
			sdk.WithChunkNumber(args.chunkNumber),
		}
		operationRequests[idx] = sdk.OperationRequest{
			FileMeta:      fileMeta,
			FileReader:    fileReader,
			OperationType: constants.FileOperationInsert,
			Opts:          options,
		}
		if args.isUpdate {
			operationRequests[idx].OperationType = constants.FileOperationUpdate
		}
	}

	err := allocationObj.DoMultiOperation(operationRequests)
	if err != nil {
		logger.Logger.Error("[update/upload]", err)
		return err
	}
	wg.Wait()
	return nil

}

// syncCmd represents sync command
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync files to/from blobbers",
	Long:  `Sync all files to/from blobbers from/to a localpath`,
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		fflags := cmd.Flags() // fflags is a *flag.FlagSet
		if fflags.Changed("localpath") == false {
			PrintError("Error: localpath flag is missing")
			os.Exit(1)
		}

		localpath := cmd.Flag("localpath").Value.String()

		if len(localpath) == 0 {
			PrintError("Error: localpath flag is missing")
			os.Exit(1)
		}

		encryptpath := cmd.Flag("encryptpath").Value.String()

		if fflags.Changed("allocation") == false { // check if the flag "path" is set
			PrintError("Error: allocation flag is missing") // If not, we'll let the user know
			os.Exit(1)                                      // and os.Exit(1)
		}
		allocationID := cmd.Flag("allocation").Value.String()

		localcache := ""
		if fflags.Changed("localcache") {
			localcache = cmd.Flag("localcache").Value.String()
		}
		exclPath := []string{}
		if fflags.Changed("excludepath") {
			exclPath, _ = cmd.Flags().GetStringArray("excludepath")
		}

		verifyDownload, err := cmd.Flags().GetBool("verifydownload")
		if err != nil {
			PrintError("Error: ", err)
			os.Exit(1)
		}

		allocationObj, err := sdk.GetAllocation(allocationID)
		if err != nil {
			PrintError("Error fetching the allocation", err)
			os.Exit(1)
		}

		fileMetas := make(map[string]*sdk.ConsolidatedFileMeta)
		wg := &sync.WaitGroup{}
		statusBar := &StatusBar{wg: wg}
		// Create filter
		filter := []string{".DS_Store", ".git"}

		uploadOnly, _ := cmd.Flags().GetBool("uploadonly")

		lDiff, err := allocationObj.GetAllocationDiff(localcache, localpath, filter, exclPath)
		if err != nil {
			PrintError("Error getting diff.", err)
			os.Exit(1)
		}

		if uploadOnly {
			var otherPaths []string
			lDiff, otherPaths = filterOperations(lDiff)
			exclPath = append(exclPath, otherPaths...)
		}

		if len(lDiff) > 0 {
			printTable(lDiff)
		} else {
			fmt.Println("Already up to date")
			saveCache(allocationObj, localcache, exclPath)
			return
		}
		argsSlice := make([]chunkedUploadArgs, 0)
		for _, f := range lDiff {
			localpath = strings.TrimRight(localpath, "/")
			lPath := localpath + f.Path
			switch f.Op {
			case sdk.Download:
				wg.Add(1)

				go func(lPath string, f sdk.FileDiff, verifyDownload bool, statusBar *StatusBar) {
					err = allocationObj.DownloadFile(lPath, f.Path, verifyDownload, statusBar)
				}(lPath, f, verifyDownload, statusBar)

			case sdk.Upload:

				encrypt := len(encryptpath) != 0 && strings.Contains(lPath, encryptpath)
				argsSlice = append(argsSlice, chunkedUploadArgs{
					localPath:     lPath,
					thumbnailPath: "",
					remotePath:    f.Path,
					encrypt:       encrypt,
					chunkNumber:   syncChunkNumber,
				})

			case sdk.Update:

				encrypt := len(encryptpath) != 0 && strings.Contains(lPath, encryptpath)
				argsSlice = append(argsSlice, chunkedUploadArgs{
					localPath:     lPath,
					thumbnailPath: "",
					remotePath:    f.Path,
					encrypt:       encrypt,
					chunkNumber:   syncChunkNumber,
					isUpdate:      true,
				})

			case sdk.Delete:
				fileMeta, err := allocationObj.GetFileMeta(f.Path)
				if err != nil {
					PrintError("Error fetching metaData :", err.Error())
				}
				fileMetas[f.Path] = fileMeta
				// TODO: User confirm??
				fmt.Printf("Deleting remote %s...\n", f.Path)
				err = allocationObj.DeleteFile(f.Path)
				if err != nil {
					PrintError("Error deleting remote file,", err.Error())
				}
				continue
			case sdk.LocalDelete:
				// TODO: User confirm??
				fmt.Printf("Deleting local %s...\n", lPath)
				err = os.RemoveAll(lPath)
				if err != nil {
					PrintError("Error deleting local file.", err.Error())
				}
				continue
			}
			if err != nil {
				PrintError(err.Error())
			}
		}
		err = startMultiUploadUpdate(allocationObj, argsSlice)
		if err != nil {
			PrintError("\nSync Failed", err.Error())
			return
		}
		wg.Wait()
		fmt.Println("\nSync Complete")
		saveCache(allocationObj, localcache, exclPath)
		return
	},
}

// The getUploadCostCmd returns value in tokens to upload a file.
var getDiffCmd = &cobra.Command{
	Use:   "get-diff",
	Short: "Get difference of local and allocation root",
	Long:  `Get difference of local and allocation root`,
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {

		fflags := cmd.Flags() // fflags is a *flag.FlagSet
		if fflags.Changed("localpath") == false {
			PrintError("Error: localpath flag is missing")
			os.Exit(1)
		}

		localpath := cmd.Flag("localpath").Value.String()

		if len(localpath) == 0 {
			PrintError("Error: localpath flag is missing")
			os.Exit(1)
		}

		if fflags.Changed("allocation") == false { // check if the flag "path" is set
			PrintError("Error: allocation flag is missing") // If not, we'll let the user know
			os.Exit(1)                                      // and os.Exit(1)
		}
		allocationID := cmd.Flag("allocation").Value.String()

		localcache := ""
		if fflags.Changed("localcache") {
			localcache = cmd.Flag("localcache").Value.String()
		}
		exclPath := []string{}
		if fflags.Changed("excludepath") {
			exclPath, _ = cmd.Flags().GetStringArray("excludepath")
		}

		allocationObj, err := sdk.GetAllocation(allocationID)
		if err != nil {
			PrintError("Error fetching the allocation", err)
			os.Exit(1)
		}

		// Create filter
		filter := []string{".DS_Store", ".git"}
		lDiff, err := allocationObj.GetAllocationDiff(localcache, localpath, filter, exclPath)
		if err != nil {
			PrintError("Error getting diff.", err)
			os.Exit(1)
		}

		util.PrintJSON(lDiff)
	},
}

// statsCmd.Flags().Bool("json", false, "pass this option to print response as json data")

var syncChunkNumber int

func init() {
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(getDiffCmd)
	syncCmd.PersistentFlags().String("allocation", "", "Allocation ID")
	syncCmd.PersistentFlags().String("localpath", "", "Local dir path to sync")
	syncCmd.PersistentFlags().String("encryptpath", "", "Local dir path to upload as encrypted")
	syncCmd.PersistentFlags().String("localcache", "", `Local cache of remote snapshot.
If file exists, this will be used for comparison with remote.
After sync complete, remote snapshot will be updated to the same file for next use.`)
	syncCmd.PersistentFlags().StringArray("excludepath", []string{}, "Remote folder paths exclude to sync")
	syncCmd.Flags().BoolP("verifydownload", "v", true, "pass this option to verify downloaded blocks")

	syncCmd.MarkFlagRequired("allocation")
	syncCmd.MarkFlagRequired("localpath")
	syncCmd.Flags().Bool("uploadonly", false, "pass this option to only upload/update the files")

	syncCmd.Flags().IntVarP(&syncChunkNumber, "chunknumber", "", 1, "how many chunks should be uploaded in a http request")

	getDiffCmd.PersistentFlags().String("allocation", "", "Allocation ID")
	getDiffCmd.PersistentFlags().String("localpath", "", "Local dir path to sync")
	getDiffCmd.PersistentFlags().String("localcache", "", `Local cache of remote snapshot.
If file exists, this will be used for comparison with remote.
After sync complete, remote snapshot will be updated to the same file for next use.`)
	getDiffCmd.PersistentFlags().StringArray("excludepath", []string{}, "Remote folder paths exclude to sync")
	getDiffCmd.MarkFlagRequired("allocation")
	getDiffCmd.MarkFlagRequired("localpath")
}
