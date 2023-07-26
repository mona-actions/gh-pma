package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/briandowns/spinner"
	"github.com/cli/go-gh"
	"github.com/cli/go-gh/pkg/api"
	"github.com/fatih/color"
	"github.com/pterm/pterm"
	"github.com/shurcooL/graphql"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
)

var (
	// flag vars
	AutoConfirm      = false
	CreateIssues     = false
	CreateCSV        = false
	GithubSourceOrg  string
	GithubTargetOrg  string
	ApiUrl           string
	GithubSourcePat  string
	GithubTargetPat  string
	DefaultBranchRef = fmt.Sprint(
		"Post-Migration Audit (PMA) Extension For GitHub CLI. Used to compare ",
		"GitHub Enterprise (Server or Cloud) to GitHub Enterprise Cloud (includes ",
		"Managed Users) migrations.",
	)

	// tool vars

	AnsiRegex                 string
	DefaultApiUrl             string = "github.com"
	DefaultIssueTitleTemplate string = "Post Migration Audit"
	SourceRestClient          api.RESTClient
	TargetRestClient          api.RESTClient
	SourceGraphqlClient       api.GQLClient
	TargetGraphqlClient       api.GQLClient
	SourceRepositories        []repository = []repository{}
	TargetRepositories        []repository = []repository{}
	ToProcessRepositories     []repository = []repository{}
	LogFile                   *os.File
	Threads                   int
	ResultsTable              pterm.TableData
	WaitGroup                 sync.WaitGroup

	// Create some colors and a spinner
	Red     = color.New(color.FgRed).SprintFunc()
	Yellow  = color.New(color.FgYellow).SprintFunc()
	Cyan    = color.New(color.FgCyan).SprintFunc()
	Pink    = color.New(color.FgHiMagenta).SprintFunc()
	Spinner = spinner.New(spinner.CharSets[2], 100*time.Millisecond)

	// Create the root cobra command
	rootCmd = &cobra.Command{
		Use:           "gh pma",
		Short:         DefaultBranchRef,
		Long:          DefaultBranchRef,
		Version:       "0.0.8",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          Process,
	}
)

type repositoryQuery struct {
	Organization struct {
		Repos repoPage `graphql:"repositories(first: 100, after: $page)"`
	} `graphql:"organization(login: $owner)"`
}
type rateResponse struct {
	Limit     int
	Remaining int
	Reset     int
	Used      int
}
type apiResponse struct {
	Resources struct {
		Core    rateResponse
		Graphql rateResponse
	}
	Message string
	Rate    rateResponse
}
type file struct {
	Content string
}
type environments struct {
	Environments []environment
	Total_Count  int
}
type environment struct {
	Name string
}
type issues struct {
	Items       []issue
	Total_Count int
}
type issue struct {
	Title  string
	ID     int
	Number int
	State  string
}
type repoPage struct {
	PageInfo struct {
		HasNextPage bool
		EndCursor   graphql.String
	}
	Nodes []repoNode
}

type branchRef struct {
	Name string
}
type repoNode struct {
	Name             string
	NameWithOwner    string
	Visibility       string
	Owner            owner
	DefaultBranchRef branchRef
}
type repository struct {
	Name             string
	Owner            string
	NameWithOwner    string
	DefaultBranchRef branchRef
	Visibility       string
	TargetVisibility string
	ExistsInTarget   bool
	Secrets          secrets
	Variables        variables
	Environments     environments
	LFS              file
}
type owner struct {
	Login string
}
type secrets struct {
	Secrets     []secret
	Total_Count int
}
type secret struct {
	Name string
}
type variables struct {
	Variables   []variable
	Total_Count int
}
type variable struct {
	Name  string
	Value string
}
type user struct {
	Login string
}

func init() {

	rootCmd.PersistentFlags().StringVar(
		&GithubSourceOrg,
		"github-source-org",
		"",
		fmt.Sprint(
			"Uses GH_SOURCE_PAT env variable or --github-source-pat option. Will ",
			"fall back to GH_PAT or --github-target-pat if not set.",
		),
	)
	rootCmd.PersistentFlags().StringVar(
		&GithubTargetOrg,
		"github-target-org",
		"",
		"Uses GH_PAT env variable or --github-target-pat option.",
	)
	rootCmd.PersistentFlags().StringVar(
		&ApiUrl,
		"ghes-api-url",
		DefaultApiUrl,
		fmt.Sprint(
			"Required if migration source is GHES. The domain name for your GHES ",
			"instance. For example: ghes.contoso.com",
		),
	)
	rootCmd.PersistentFlags().StringVar(
		&GithubSourcePat,
		"github-source-pat",
		"",
		"",
	)
	rootCmd.PersistentFlags().StringVar(
		&GithubTargetPat,
		"github-target-pat",
		"",
		"",
	)
	rootCmd.PersistentFlags().IntVar(
		&Threads,
		"threads",
		3,
		fmt.Sprint(
			"Number of threads to process concurrently. Maximum of 10 allowed. ",
			"Increasing this number could get your PAT blocked due to API limiting.",
		),
	)
	rootCmd.PersistentFlags().BoolVar(
		&AutoConfirm,
		"confirm",
		false,
		"Auto respond to visibility alignment confirmation prompt",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&CreateIssues,
		"create-issues",
		"i",
		false,
		"Whether to create issues in target org repositories or not.",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&CreateCSV,
		"create-csv",
		"c",
		false,
		"Whether to create a CSV file with the results.",
	)

	// make certain flags required
	rootCmd.MarkPersistentFlagRequired("github-source-org")
	rootCmd.MarkPersistentFlagRequired("github-target-org")

	// update version template to show extension name
	rootCmd.SetVersionTemplate(
		fmt.Sprintf("PMA Extension for GitHub CLI, v%s\n", rootCmd.Version),
	)

	// add args here
	rootCmd.Args = cobra.MaximumNArgs(0)

	// set ANSI regex
	AnsiRegex = "[\u001B\u009B]"
	AnsiRegex += "[[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]"
	AnsiRegex += "*(?:;[a-zA-Z\\d]*)*)?\u0007)"
	AnsiRegex += "|(?:(?:\\d{1,4}(?:;\\d{0,4})*)"
	AnsiRegex += "?[\\dA-PRZcf-ntqry=><~]))"
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		ExitOnError(err)
	}
}

func ExitOnError(err error) {
	if err != nil {
		rootCmd.PrintErrln(err.Error())
		os.Exit(1)
	}
}

func ExitManual(err error) {
	Spinner.Stop()
	fmt.Println(err.Error())
	os.Exit(1)
}

func OutputFlags(key string, value string) {
	sep := ": "
	fmt.Println(fmt.Sprint(Pink(key), sep, value))
	Log(fmt.Sprint(key, sep, value))
}

func OutputNotice(message string) {
	Output(message, "default", false, false)
}

func OutputWarning(message string) {
	Output(fmt.Sprint("[WARNING] ", message), "yellow", false, false)
}

func OutputError(message string, exit bool) {
	Spinner.Stop()
	Output(message, "red", true, exit)
}

func Output(message string, color string, isErr bool, exit bool) {

	if isErr {
		message = fmt.Sprint("[ERROR] ", message)
	}
	Log(StripAnsi(message))

	switch {
	case color == "red":
		message = Red(message)
	case color == "yellow":
		message = Yellow(message)
	}
	fmt.Println(message)
	if exit {
		fmt.Println("")
		os.Exit(1)
	}
}

func AskForConfirmation(s string) (res bool, err error) {
	// read the input
	reader := bufio.NewReader(os.Stdin)
	// loop until a response is valid
	for {
		fmt.Printf("%s [y/n]: ", s)
		response, err := reader.ReadString('\n')
		Debug(fmt.Sprint("User responded with: ", response))
		if err != nil {
			return false, err
		}
		response = strings.ToLower(strings.TrimSpace(response))
		if response == "y" || response == "yes" {
			return true, err
		} else if response == "n" || response == "no" {
			return false, err
		}
	}
}

func DebugAndStatus(message string) string {
	Spinner.Suffix = fmt.Sprint(
		" ",
		message,
	)
	return Debug(message)
}

func Debug(message string) string {
	Log(message)
	return message
}

func DisplayRateLeft() {
	// validate we have API attempts left
	rateLeft, _ := ValidateApiRate(SourceRestClient, "core")
	rateMessage := Cyan("API Rate Limit Left:")
	OutputNotice(fmt.Sprintf("%s %d", rateMessage, rateLeft))
	LF()
}

func IsTargetProvided() bool {
	if GithubTargetOrg != "" {
		return true
	}
	return false
}

func GetOpts(hostname, token string) (options api.ClientOptions) {
	// set options
	opts := api.ClientOptions{
		Host:     hostname,
		CacheTTL: time.Hour,
	}
	if token != "" {
		opts.AuthToken = token
	}
	return opts
}

func LF() {
	fmt.Println("")
}

func LFSExists(base64File string) (string, bool, error) {
	decodedContents, err := base64.StdEncoding.DecodeString(base64File)
	if err != nil {
		return "", false, err
	}
	if strings.Contains(string(decodedContents), "filter=lfs") {
		return string(decodedContents), true, err
	}
	return string(decodedContents), false, err
}

func Log(message string) {
	if message != "" {
		message = fmt.Sprint(
			"[",
			time.Now().Format("2006-01-02 15:04:05"),
			"] ",
			message,
		)
	}
	_, err := LogFile.WriteString(
		fmt.Sprintln(message),
	)
	if err != nil {
		fmt.Println(Red("Unable to write to log file."))
		fmt.Println(Red(err))
		os.Exit(1)
	}
}

func Truncate(str string, limit int) string {
	lastSpaceIx := -1
	len := 0
	for i, r := range str {
		if unicode.IsSpace(r) {
			lastSpaceIx = i
		}
		len++
		if len >= limit {
			if lastSpaceIx != -1 {
				return fmt.Sprint(str[:lastSpaceIx], "...")
			} else {
				return fmt.Sprint(str[:limit], "...")
			}
		}
	}
	return str
}

func StripAnsi(str string) string {
	regex := regexp.MustCompile(AnsiRegex)
	return regex.ReplaceAllString(str, "")
}

func SleepIfLongerThan(thisTime time.Time) {
	// delay if write was fast
	elapsed := time.Since(thisTime)
	if elapsed.Seconds() < 1 {
		wait := 1000 - elapsed.Milliseconds()
		DebugAndStatus(
			fmt.Sprintf(
				"Execution time was %v. Waiting for %vms to avoid rate limiting.",
				elapsed.Seconds(),
				wait,
			),
		)
		time.Sleep(time.Duration(wait) * time.Millisecond)
	}
}

func Process(cmd *cobra.Command, args []string) (err error) {

	// Create log file
	LogFile, err = os.Create(
		fmt.Sprint(
			time.Now().Format("20060102150401"),
			".pma.log",
		),
	)
	if err != nil {
		return err
	}
	defer LogFile.Close()

	Debug("---- VALIDATING FLAGS & ENV VARS ----")

	if GithubSourcePat == "" {
		GithubSourcePatEnv, isSet := os.LookupEnv("GH_SOURCE_PAT")
		if isSet {
			GithubSourcePat = GithubSourcePatEnv
			Debug("Source PAT set from Environment Variable GH_SOURCE_PAT")
		} else {
			OutputError(
				fmt.Sprint(
					"A source PAT was not provided via --github-source-pat or ",
					"environment variable GH_SOURCE_PAT",
				),
				true,
			)
		}
	}

	if GithubTargetPat == "" {
		GithubTargetPatEnv, isSet := os.LookupEnv("GH_PAT")
		if isSet {
			GithubTargetPat = GithubTargetPatEnv
			Debug("Target PAT set from Environment Variable GH_PAT")
		} else {
			OutputError(
				fmt.Sprint(
					"A target PAT was not provided via --github-target-pat or ",
					"environment variable GH_PAT",
				),
				true,
			)
		}
	}

	// validate API URL
	r, _ := regexp.Compile("^http(s|):(//|)")
	if r.MatchString(ApiUrl) {
		OutputError(
			"--ghes-api-url should NOT contain http(s).",
			true,
		)
	}

	if Threads > 10 {
		OutputError("Number of concurrent threads cannot be higher than 10.", true)
	} else if Threads > 3 {
		OutputWarning(
			fmt.Sprint(
				"Number of concurrent threads is higher than 3. This could result in ",
				"extreme load on your server.",
			),
		)
	}
	LF()

	// output flags for reference
	OutputFlags("GitHub Source Org", GithubSourceOrg)
	if ApiUrl != DefaultApiUrl {
		OutputFlags("GHES Source URL", ApiUrl)
	}
	if IsTargetProvided() {
		OutputFlags("GitHub Target Org", GithubTargetOrg)
	}
	OutputFlags("Read Threads", fmt.Sprintf("%d", Threads))
	LF()
	Debug("---- SETTING UP API CLIENTS ----")

	// set up clients
	opts := GetOpts(ApiUrl, GithubSourcePat)
	SourceRestClient, err = gh.RESTClient(&opts)
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed to set up source REST client.", true)
	}

	SourceGraphqlClient, err = gh.GQLClient(&opts)
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed set set up source GraphQL client.", true)
	}

	opts = GetOpts(DefaultApiUrl, GithubTargetPat)
	TargetRestClient, err = gh.RESTClient(&opts)
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed to set up target REST client.", true)
	}

	TargetGraphqlClient, err = gh.GQLClient(&opts)
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed set set up target GraphQL client.", true)
	}

	Debug("---- LOOKING UP REPOSITORIES IN ORGS ----")

	Spinner.Start()

	// thread getting repos
	WaitGroup.Add(2)
	go GetSourceRepositories()
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed to get source repositories.", true)
	}
	go GetTargetRepositories()
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed to get target repositories.", true)
	}
	WaitGroup.Wait()

	Debug(fmt.Sprintf("Found %d repositories", len(SourceRepositories)))
	Debug("---- GETTING REPOSITORY DATA ----")

	// set up table header for displaying of data
	Debug("Creating table data for display...")
	ResultsTable = pterm.TableData{
		{
			"Repository",
			"Exists In Target",
			"Default Branch",
			"Visibility",
			"Secrets",
			"Variables",
			"Environments",
		},
	}

	// set a temp var that we can batch through without effecting the original
	repositoriesToProcess := SourceRepositories
	batchThreads := Threads
	batchNum := 1

	for len(repositoriesToProcess) > 0 {

		repositoriesLeft := len(repositoriesToProcess)
		if repositoriesLeft < Threads {
			batchThreads = repositoriesLeft
			Debug(
				fmt.Sprint(
					"Setting number of threads to",
					fmt.Sprintf(
						" %d because there are only %d repositories left.",
						repositoriesLeft,
						repositoriesLeft,
					),
				),
			)
		}

		DebugAndStatus(
			fmt.Sprintf(
				"Running repository analysis batch #%d (%d threads)...",
				batchNum,
				batchThreads,
			),
		)

		// get the next batch into new array and remove from processing array
		batch := repositoriesToProcess[:batchThreads]
		repositoriesToProcess = repositoriesToProcess[len(batch):]

		// add the number of wait groups needed
		WaitGroup.Add(len(batch))

		// process threads
		for i := 0; i < len(batch); i++ {
			Debug(
				fmt.Sprintf(
					"Running thread %d of %d on repository '%s'",
					i+1,
					len(batch),
					batch[i].NameWithOwner,
				),
			)
			go GetRepositoryStatistics(SourceRestClient, batch[i])
		}

		// wait for threads to finish
		WaitGroup.Wait()
		batchNum++
	}

	Spinner.Stop()

	// output table
	if len(SourceRepositories) > 0 {
		pterm.DefaultTable.WithHasHeader().
			WithHeaderRowSeparator("-").
			WithData(ResultsTable).Render()
	} else {
		OutputNotice("No repositories found.")
		return err
	}

	// Create output file
	if CreateCSV {
		outputFile, err := os.Create(
			fmt.Sprint(
				time.Now().Format("20060102150401"),
				".",
				GithubSourceOrg,
				".csv",
			),
		)
		if err != nil {
			return err
		}
		defer outputFile.Close()

		// write header
		_, err = outputFile.WriteString(
			fmt.Sprintln(
				"repository,",
				"exists_in_target,",
				"default_branch,",
				"visibility,",
				"secrets,",
				"variables,",
				"environments",
				"lfs",
			),
		)
		if err != nil {
			OutputError("Error writing to output file.", true)
		}
		// write body
		for _, repository := range SourceRepositories {
			line := fmt.Sprintf("%s", repository.NameWithOwner)
			if !IsTargetProvided() {
				line = fmt.Sprintf("%s,%s", line, "Unknown")
			} else {
				line = fmt.Sprintf("%s,%t", line, repository.ExistsInTarget)
			}
			_, foundLFS, _ := LFSExists(repository.LFS.Content)
			lfsString := "No"
			if foundLFS {
				lfsString = "Potentially"
			}
			line = fmt.Sprintf(
				"%s,%s,%s|%s,%d,%d,%d,%s\n",
				line,
				repository.DefaultBranchRef.Name,
				repository.Visibility,
				repository.TargetVisibility,
				repository.Secrets.Total_Count,
				repository.Variables.Total_Count,
				repository.Environments.Total_Count,
				lfsString,
			)
			_, err = outputFile.WriteString(line)
			if err != nil {
				OutputError("Error writing to output file.", true)
			}
		}
	}

	// create issues if we need to
	if CreateIssues {
		Spinner.Start()
		DebugAndStatus("Attempting to create issues for repositories...")
		err = ProcessIssues(SourceRestClient, GithubTargetOrg, SourceRepositories)
		Spinner.Stop()
		if err != nil {
			return err
		}
	}

	// prompt for fixing
	if len(ToProcessRepositories) > 0 {

		// find out if we need to process visibility
		repoWord := "repository"
		if len(ToProcessRepositories) > 1 {
			repoWord = "repositories"
		}
		proceedMessage := Debug(fmt.Sprintf(
			"Do you want to align visibility for %d %s?",
			len(ToProcessRepositories),
			repoWord,
		))

		// auto confirm
		c := true
		if !AutoConfirm {
			c, err = AskForConfirmation(Yellow(proceedMessage))
		}
		if err != nil {
			OutputError(err.Error(), true)
		} else if !c {
			// warn when manually abandoned
			LF()
			OutputWarning("Alignment process abandoned.")
			LF()
			DisplayRateLeft()
			return err
		}

		// process if code gets to here
		Spinner.Start()
		err = ProcessRepositoryVisibilities(
			TargetRestClient,
			GithubTargetOrg,
			ToProcessRepositories,
		)
		Spinner.Stop()

		// on successful processing
		if err == nil {
			LF()
			OutputNotice(
				fmt.Sprintf(
					"Successfully processed %d repositories.",
					len(ToProcessRepositories),
				),
			)
			LF()
		}
	}

	DisplayRateLeft()

	// always return
	return err
}

func ValidateApiRate(
	client api.RESTClient,
	requestType string,
) (left int, err error) {

	apiResponse := apiResponse{}
	attempts := 0
	rateRemaining := 0

	for {

		// after 240 attempts (1 hour), end the scrip.
		if attempts >= 240 {
			return 0, errors.New(
				fmt.Sprint(
					"After an hour of retrying, the API rate limit has not ",
					"refreshed. Aborting.",
				),
			)
		}

		// get the current rate liit left or error out if request fails
		err = client.Get("rate_limit", &apiResponse)
		if err != nil {
			Debug("Failed to get rate limit from GitHub server.")
			return 0, err
		}

		// if rate limiting is disabled, do not proceed
		if apiResponse.Message == "Rate limiting is not enabled." {
			Debug("Rate limit is not enabled.")
			return 0, err
		}
		// choose which response to validate
		switch {
		default:
			return 0, errors.New(
				fmt.Sprintf(
					"Invalid API request type provided: '%s'",
					requestType,
				),
			)
		case requestType == "core":
			rateRemaining = apiResponse.Resources.Core.Remaining
		case requestType == "graphql":
			rateRemaining = apiResponse.Resources.Graphql.Remaining
		}
		// validate there is rate left
		if rateRemaining <= 0 {
			attempts++
			DebugAndStatus(
				fmt.Sprint(
					"API rate limit ",
					fmt.Sprintf(
						"(%s) has none remaining. Sleeping for 15 seconds (attempt #%d)",
						requestType,
						attempts,
					),
				),
			)
			time.Sleep(15 * time.Second)
		} else {
			break
		}
	}
	return rateRemaining, err
}

func GetRepositoryStatistics(client api.RESTClient, repoToProcess repository) {

	// validate we have API attempts left
	_, timeoutErr := ValidateApiRate(client, "core")
	if timeoutErr != nil {
		OutputError(timeoutErr.Error(), true)
	}

	// start timer
	start := time.Now()

	// get number of secrets
	var secretsResponse secrets
	secretsErr := client.Get(
		fmt.Sprintf(
			"repos/%s/actions/secrets",
			repoToProcess.NameWithOwner,
		),
		&secretsResponse,
	)
	Debug(fmt.Sprintf(
		"Secrets from %s: %+v",
		repoToProcess.NameWithOwner,
		secretsResponse,
	))
	if secretsErr != nil {
		ExitManual(secretsErr)
	} else {
		repoToProcess.Secrets = secretsResponse
	}

	// validate we have API attempts left

	_, timeoutErr = ValidateApiRate(client, "core")
	if timeoutErr != nil {
		OutputError(timeoutErr.Error(), true)
	}

	// get number of variables
	var variablesResponse variables
	variablesErr := client.Get(
		fmt.Sprintf(
			"repos/%s/actions/variables",
			repoToProcess.NameWithOwner,
		),
		&variablesResponse,
	)
	Debug(fmt.Sprintf(
		"Variables from %s: %+v",
		repoToProcess.NameWithOwner,
		variablesResponse,
	))
	if variablesErr != nil {
		ExitManual(variablesErr)
	} else {
		repoToProcess.Variables = variablesResponse
	}

	// validate we have API attempts left
	_, timeoutErr = ValidateApiRate(client, "core")
	if timeoutErr != nil {
		OutputError(timeoutErr.Error(), true)
	}

	// get number of variables
	var envResponse environments
	envsErr := client.Get(
		fmt.Sprintf(
			"repos/%s/environments",
			repoToProcess.NameWithOwner,
		),
		&envResponse,
	)
	Debug(fmt.Sprintf(
		"Environments from %s: %+v",
		repoToProcess.NameWithOwner,
		envResponse,
	))
	if envsErr != nil {
		ExitManual(envsErr)
	} else {
		repoToProcess.Environments = envResponse
	}

	// get LFS definition file
	var fileResponse file
	_ = client.Get(
		fmt.Sprintf(
			"repos/%s/contents/.gitattributes",
			repoToProcess.NameWithOwner,
		),
		&fileResponse,
	)
	Debug(fmt.Sprintf(
		"Contents of .gitattributes from %s: %+v",
		repoToProcess.NameWithOwner,
		fileResponse,
	))
	repoToProcess.LFS = fileResponse

	// find if repo exists in target
	tIdx := slices.IndexFunc(TargetRepositories, func(r repository) bool {
		return r.Name == repoToProcess.Name
	})
	if tIdx < 0 {
		repoToProcess.ExistsInTarget = false
		repoToProcess.TargetVisibility = fmt.Sprintf("UNKNOWN")
	} else {
		repoToProcess.ExistsInTarget = true
		repoToProcess.TargetVisibility = TargetRepositories[tIdx].Visibility

		// add this repo to array of processing if visibilties don't match
		if repoToProcess.Visibility != TargetRepositories[tIdx].Visibility {
			ToProcessRepositories = append(ToProcessRepositories, repoToProcess)
		}
	}

	// find index of repo in original list and overwite it
	idx := slices.IndexFunc(SourceRepositories, func(r repository) bool {
		return r.NameWithOwner == repoToProcess.NameWithOwner
	})
	if idx < 0 {
		OutputError(
			fmt.Sprintf(
				"Error finding batch repository in original list: %s",
				repoToProcess.NameWithOwner,
			),
			false,
		)
	} else {
		SourceRepositories[idx] = repoToProcess
	}

	// write to table for output
	visiblity := fmt.Sprintf(
		"%s",
		repoToProcess.Visibility,
	)
	existsInTarget := strconv.FormatBool(repoToProcess.ExistsInTarget)
	if !repoToProcess.ExistsInTarget {
		existsInTarget = Red(existsInTarget)
	} else if repoToProcess.Visibility != TargetRepositories[tIdx].Visibility {
		visiblity = fmt.Sprintf(
			"%s|%s",
			visiblity,
			Yellow(repoToProcess.TargetVisibility),
		)
	}
	ResultsTable = append(ResultsTable, []string{
		repoToProcess.NameWithOwner,
		existsInTarget,
		repoToProcess.DefaultBranchRef.Name,
		strings.ToLower(visiblity),
		fmt.Sprintf("%d", secretsResponse.Total_Count),
		fmt.Sprintf("%d", variablesResponse.Total_Count),
		fmt.Sprintf("%d", envResponse.Total_Count),
	})

	SleepIfLongerThan(start)

	// close out this thread
	WaitGroup.Done()
}

func GetSourceRepositories() {
	repositoryQueryResults, err := GetRepositories(
		SourceRestClient,
		SourceGraphqlClient,
		GithubSourceOrg,
	)
	if err != nil {
		ExitManual(err)
	}
	SourceRepositories = repositoryQueryResults
	WaitGroup.Done()
}

func GetTargetRepositories() {
	repositoryQueryResults, err := GetRepositories(
		TargetRestClient,
		TargetGraphqlClient,
		GithubTargetOrg,
	)
	if err != nil {
		ExitManual(err)
	}
	TargetRepositories = repositoryQueryResults
	WaitGroup.Done()
}

func GetRepositories(
	restClient api.RESTClient,
	graphqlClient api.GQLClient,
	owner string,
) ([]repository, error) {

	repoLookup := []repository{}
	query := repositoryQuery{}
	var err error

	// get our variables set up for the graphql query
	variables := map[string]interface{}{
		"owner": graphql.String(owner),
		"page":  (*graphql.String)(nil),
	}

	// Loop through pages of repositories, waiting 1 second in between
	var i = 1
	for {

		// validate we have API attempts left
		_, err := ValidateApiRate(restClient, "graphql")
		if err != nil {
			OutputError(err.Error(), true)
		}

		// show a suffix next to the spinner for what we are curretnly doing
		DebugAndStatus(
			fmt.Sprintf(
				"Fetching repositories from organization '%s' (page %d)",
				owner,
				i,
			),
		)

		// make the graphql request
		graphqlClient.Query("RepoList", &query, variables)

		// clone the objects (keeping just the name)
		for _, repoNode := range query.Organization.Repos.Nodes {
			var repoClone repository
			repoClone.Name = repoNode.Name
			repoClone.Owner = repoNode.Owner.Login
			repoClone.NameWithOwner = repoNode.NameWithOwner
			repoClone.Visibility = repoNode.Visibility
			repoClone.DefaultBranchRef = repoNode.DefaultBranchRef
			repoLookup = append(repoLookup, repoClone)
		}

		Debug(
			fmt.Sprintf(
				"GraphQL Repository Query Response: %+v",
				query.Organization.Repos,
			),
		)

		// if no next page is found, break
		if !query.Organization.Repos.PageInfo.HasNextPage {
			break
		}
		i++

		// set the end cursor for the page we are on
		variables["page"] = query.Organization.Repos.PageInfo.EndCursor
	}

	return repoLookup, err
}

func ProcessIssues(
	client api.RESTClient,
	targetOrg string,
	reposToProcess []repository,
) (err error) {

	for _, repository := range reposToProcess {

		var issuesResponse issues

		if !repository.ExistsInTarget {
			Debug(
				fmt.Sprintf(
					"Skipped %s because it did not exist in the target org.",
					repository.Name,
				),
			)
			continue
		}

		// validate rate
		_, err := ValidateApiRate(client, "core")
		if err != nil {
			return err
		}

		query := url.QueryEscape(
			fmt.Sprintf(
				"%s repo:%s/%s in:title state:open",
				DefaultIssueTitleTemplate,
				targetOrg,
				repository.Name,
			),
		)
		Debug(fmt.Sprintf("Using search string: %s", query))
		DebugAndStatus(
			fmt.Sprintf(
				"Searching issues in %s/%s",
				targetOrg,
				repository.Name,
			),
		)
		err = client.Get(
			fmt.Sprintf(
				"search/issues?q=%s",
				query,
			),
			&issuesResponse,
		)
		Debug(fmt.Sprintf("Response from GET: %+v", issuesResponse))

		if err != nil {
			return err
		}

		// validate rate
		_, err = ValidateApiRate(client, "core")
		if err != nil {
			return err
		}

		// start timer
		start := time.Now()

		// use json marshal indention for JSON
		varIndented, _ := json.MarshalIndent(repository.Variables, "", "  ")
		secIndented, _ := json.MarshalIndent(repository.Secrets, "", "  ")
		envIndented, _ := json.MarshalIndent(repository.Environments, "", "  ")

		// set some time zone info
		now := time.Now()
		tz, _ := now.Zone()

		// create a template
		issueTemplate := `# Audit Results
Audit last performed on %s at %s %s.

See below for migration details and whether you need to mitigate any items.

## Details
- **Migrated From:** [%s](https://github.com/%s)
- **Source Visibility:** [%s](https://github.com/%s/settings/#danger-zone)

## Items From Source
### [Variables](https://github.com/%s/settings/secrets/actions)

%s
### [Secrets](https://github.com/%s/settings/secrets/variables)

%s
### [Environments](https://github.com/%s/settings/environments)

%s
## LFS Detection
*Only accounts for %s branch.*

%s`

		// build a string for LFS output ahead of time
		decodedLFS, foundLFS, _ := LFSExists(repository.LFS.Content)
		lfsString := "No LFS declaration detected in `.gitattributes`\n"
		if foundLFS {
			lfsString = "Validate the paths referenced in `.gitattributes`:\n"
			lfsString += "```\n"
			lfsString += string(decodedLFS)
			lfsString += "```\n"
		}

		// replace values in template
		newRepoAndOwner := fmt.Sprint(targetOrg, "/", repository.Name)
		issueBody := fmt.Sprintf(
			issueTemplate,
			now.Format("2006-01-02"),
			now.Format("15:04:05"),
			tz,
			repository.Owner,
			repository.NameWithOwner,
			strings.ToLower(repository.Visibility),
			newRepoAndOwner,
			newRepoAndOwner,
			fmt.Sprint("```\n", string(varIndented), "\n```"),
			newRepoAndOwner,
			fmt.Sprint("```\n", string(secIndented), "\n```"),
			newRepoAndOwner,
			fmt.Sprint("```\n", string(envIndented), "\n```"),
			repository.DefaultBranchRef.Name,
			lfsString,
		)

		if issuesResponse.Total_Count == 1 {

			var updateResponse interface{}
			// find issue number
			foundIssue := issuesResponse.Items[0]
			// update an issue
			updateBody, err := json.Marshal(map[string]string{
				"body": issueBody,
			})
			if err != nil {
				return err
			}
			updateUrl := fmt.Sprintf(
				"repos/%s/%s/issues/%d",
				targetOrg,
				repository.Name,
				foundIssue.Number,
			)
			DebugAndStatus(fmt.Sprintf("Updating issue on %s", updateUrl))
			err = client.Patch(
				updateUrl,
				bytes.NewBuffer(updateBody),
				updateResponse,
			)
			if err != nil {
				return err
			}
			Debug(fmt.Sprintf("Response from PATCH: %+v", updateResponse))

		} else if issuesResponse.Total_Count == 0 {

			var createResponse interface{}
			// create an issue
			createBody, err := json.Marshal(map[string]string{
				"title": DefaultIssueTitleTemplate,
				"body":  issueBody,
			})
			if err != nil {
				return err
			}
			createUrl := fmt.Sprintf(
				"repos/%s/%s/issues",
				targetOrg,
				repository.Name,
			)
			DebugAndStatus(fmt.Sprintf("Creating issue on %s", createUrl))
			err = client.Post(
				createUrl,
				bytes.NewBuffer(createBody),
				createResponse,
			)
			if err != nil {
				return err
			}
			Debug(fmt.Sprintf("Response from POST: %+v", createResponse))

		} else {

			// when more than 1 issue is found
			Debug(fmt.Sprint(
				"Could not accurately determine issue to update because multiple ",
				fmt.Sprintf(
					"issues with the title '%s' were found.",
					DefaultIssueTitleTemplate,
				),
			))
		}

		SleepIfLongerThan(start)
	}

	return err
}

func ProcessRepositoryVisibilities(
	client api.RESTClient,
	targetOrg string,
	reposToProcess []repository,
) (err error) {

	var response interface{}
	for _, repository := range reposToProcess {

		// validate rate
		_, err := ValidateApiRate(client, "core")
		if err != nil {
			return err
		}

		// create json body
		requestbody, err := json.Marshal(map[string]string{
			"visibility": strings.ToLower(repository.Visibility),
		})
		if err != nil {
			return err
		}

		// start timer
		start := time.Now()

		// perform request
		DebugAndStatus(
			fmt.Sprintf(
				"Patching %s/%s with visibility '%s'",
				targetOrg,
				repository.NameWithOwner,
				strings.ToLower(repository.Visibility),
			),
		)
		err = client.Patch(
			fmt.Sprintf(
				"repos/%s/%s",
				targetOrg,
				repository.Name,
			),
			bytes.NewBuffer(requestbody),
			&response,
		)
		Debug(fmt.Sprintf("Response from PATCH: %+v", response))
		if err != nil {
			return err
		}

		SleepIfLongerThan(start)
	}

	// always return
	return err
}
