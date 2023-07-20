package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
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
	GithubSourceOrg string
	GithubTargetOrg string
	ApiUrl          string
	GithubSourcePat string
	GithubTargetPat string
	NoSslVerify     = false
	Description     = fmt.Sprint(
		"Post-Migration Audit (PMA) Extension For GitHub CLI. Used to compare ",
		"GitHub Enterprise (Server or Cloud) to GitHub Enterprise Cloud (includes ",
		"Managed Users) migrations.",
	)

	// tool vars
	DefaultApiUrl    string = "github.com"
	SourceRestClient api.RESTClient
	TargetRestClient api.RESTClient
	GraphqlClient    api.GQLClient
	RepositoryQuery  struct {
		Organization struct {
			Repositories repositoriesPage `graphql:"repositories(first: 100, after: $page, orderBy: {field: NAME, direction: ASC})"`
		} `graphql:"organization(login: $owner)"`
	}
	Repositories []repository = []repository{}
	LogFile      *os.File
	Threads      int
	ResultsTable pterm.TableData
	WaitGroup    sync.WaitGroup

	// Create some colors and a spinner
	Red     = color.New(color.FgRed).SprintFunc()
	Yellow  = color.New(color.FgYellow).SprintFunc()
	Cyan    = color.New(color.FgCyan).SprintFunc()
	Pink    = color.New(color.FgHiMagenta).SprintFunc()
	Spinner = spinner.New(spinner.CharSets[2], 100*time.Millisecond)

	// Create the root cobra command
	rootCmd = &cobra.Command{
		Use:           "gh pma",
		Short:         Description,
		Long:          Description,
		Version:       "0.0.1",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          Process,
	}
)

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
type environments struct {
	Environments []environment
}
type environment struct {
	Name string
}
type repositoriesPage struct {
	PageInfo struct {
		HasNextPage bool
		EndCursor   graphql.String
	}
	Nodes []repositoryNode
}
type repositoryNode struct {
	Name          string
	NameWithOwner string
	Owner         organization
	Description   string
	URL           string
}
type repository struct {
	NameWithOwner string
	Secrets       int
	Variables     int
	Environments  int
}
type organization struct {
	Login string
}
type secrets struct {
	Secrets []secret
}
type secret struct {
	Name string
}
type variables struct {
	Variables []secret
}
type variable struct {
	Name string
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
			"Required if migrating from GHES. The domain name for your GHES ",
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
	rootCmd.PersistentFlags().BoolVar(
		&NoSslVerify,
		"no-ssl-verify",
		false,
		fmt.Sprint(
			"Only effective if migrating from GHES. Disables SSL verification when ",
			"communicating with your GHES instance. All other migration steps will ",
			"continue to verify SSL. If your GHES instance has a self-signed SSL ",
			"certificate then setting this flag will allow data to be extracted.",
		),
	)
	rootCmd.PersistentFlags().IntVarP(
		&Threads,
		"threads",
		"t",
		3,
		fmt.Sprint(
			"Number of threads to process concurrently. Maximum of 10 allowed. ",
			"Increasing this number could get your PAT blocked due to API limiting.",
		),
	)

	// make certain flags required
	rootCmd.MarkPersistentFlagRequired("github-source-org")
	//rootCmd.MarkPersistentFlagRequired("github-target-org")

	// add args here
	rootCmd.Args = cobra.MaximumNArgs(0)
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
	Log(message)

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

func LF() {
	Output("", "default", false, false)
}

func LogLF() {
	Log("")
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

func Process(cmd *cobra.Command, args []string) (err error) {

	// Create log file
	LogFile, err = os.Create(fmt.Sprint(time.Now().Format("20060102150401"), ".pma.log"))
	if err != nil {
		return err
	}
	defer LogFile.Close()

	LF()
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

	// output flags for reference
	OutputFlags("GitHub Source Org", GithubSourceOrg)
	if ApiUrl != DefaultApiUrl {
		OutputFlags("GHES Source URL", ApiUrl)
	}
	if GithubTargetOrg != "" {
		OutputFlags("GitHub Target Org", GithubTargetOrg)
	}
	if NoSslVerify {
		OutputFlags("SSL Verification Disabled", strconv.FormatBool(NoSslVerify))
	}
	OutputFlags("Threads", fmt.Sprintf("%d", Threads))

	Debug("---- LISTING REPOSITORIES ----")

	// set up clients
	opts := GetOpts(ApiUrl, GithubSourcePat)
	SourceRestClient, err = gh.RESTClient(&opts)
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed to set up source REST client.", true)
	}

	GraphqlClient, err := gh.GQLClient(&opts)
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed set set up GraphQL client.", true)
	}

	opts = GetOpts(DefaultApiUrl, GithubTargetPat)
	TargetRestClient, err = gh.RESTClient(&opts)
	if err != nil {
		Debug(fmt.Sprint("Error object: ", err))
		OutputError("Failed to set up target REST client.", true)
	}
	LF()

	Spinner.Start()

	// Loop through pages of repositories, waiting 1 second in between
	var i = 1
	for {

		// validate we have API attempts left
		timeoutErr := ValidateApiRate(SourceRestClient, "graphql")
		if timeoutErr != nil {
			OutputError(timeoutErr.Error(), true)
		}

		// show a suffix next to the spinner for what we are curretnly doing
		DebugAndStatus(
			fmt.Sprintf(
				"Fetching repositories from organization '%s' (page %d)",
				GithubSourceOrg,
				i,
			),
		)

		// get our variables set up for the graphql query
		variables := map[string]interface{}{
			"owner": graphql.String(GithubSourceOrg),
			"page":  (*graphql.String)(nil),
		}

		// make the graphql request
		GraphqlClient.Query("RepoList", &RepositoryQuery, variables)

		// clone the objects (keeping just the name)
		for _, repoNode := range RepositoryQuery.Organization.Repositories.Nodes {
			var repoClone repository
			repoClone.NameWithOwner = repoNode.NameWithOwner
			Repositories = append(Repositories, repoClone)
		}

		// if no next page is found, break
		if !RepositoryQuery.Organization.Repositories.PageInfo.HasNextPage {
			break
		}
		i++

		// set the end cursor for the page we are on
		variables["page"] = &RepositoryQuery.Organization.Repositories.PageInfo.EndCursor
	}

	Debug(fmt.Sprintf("Found %d repositories", len(Repositories)))
	Debug("---- GETTING REPOSITORY DATA ----")

	// set up table header for displaying of data
	Debug("Creating table data for display...")
	ResultsTable = pterm.TableData{
		{
			"Repository",
			"Secrets",
			"Variables",
			"Environments",
		},
	}

	// set a temp var that we can batch through without effecting the original
	repositoriesToProcess := Repositories
	batchThreads := Threads
	batchNum := 1

	for len(repositoriesToProcess) > 0 {

		repositoriesLeft := len(repositoriesToProcess)
		if repositoriesLeft < Threads {
			batchThreads = repositoriesLeft
			Debug(
				fmt.Sprintf(
					"Setting number of threads to %d because there are only %d repositories left.",
					repositoriesLeft,
					repositoriesLeft,
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
			go GetRepositoryStatistics(batch[i])
		}

		// wait for threads to finish
		WaitGroup.Wait()
		batchNum++
	}

	Spinner.Stop()

	// output table
	if len(Repositories) > 0 {
		pterm.DefaultTable.WithHasHeader().WithHeaderRowSeparator("-").WithData(ResultsTable).Render()
	} else {
		OutputNotice("No repositories found.")
		LF()
	}

	// Create output file
	outputFile, err := os.Create(fmt.Sprint(time.Now().Format("20060102150401"), ".", GithubSourceOrg, ".csv"))
	if err != nil {
		return err
	}
	defer outputFile.Close()

	// write header
	_, err = outputFile.WriteString(
		fmt.Sprintln("repository,secrets,variables,environments"),
	)
	if err != nil {
		OutputError("Error writing to output file.", true)
	}
	// write body
	for _, repository := range Repositories {
		_, err = outputFile.WriteString(
			fmt.Sprintln(
				fmt.Sprintf(
					"%s,%d,%d,%d",
					repository.NameWithOwner,
					repository.Secrets,
					repository.Variables,
					repository.Environments,
				),
			),
		)
		if err != nil {
			OutputError("Error writing to output file.", true)
		}
	}

	// always return
	return err
}

func ValidateApiRate(client api.RESTClient, requestType string) (err error) {
	apiResponse := apiResponse{}
	attempts := 0

	for {

		// after 240 attempts (1 hour), end the scrip.
		if attempts >= 240 {
			return errors.New(
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
			return err
		}

		// if rate limiting is disabled, do not proceed
		if apiResponse.Message == "Rate limiting is not enabled." {
			Debug("Rate limit is not enabled.")
			return err
		}
		// choose which response to validate
		rateRemaining := 0
		switch {
		default:
			return errors.New(
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
				fmt.Sprintf(
					fmt.Sprintf(
						"API rate limit (%s) has none remaining. Sleeping for 15 seconds (attempt #%d)",
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
	return err
}

func GetRepositoryStatistics(repoToProcess repository) {

	Debug("is this working?")

	// validate we have API attempts left
	timeoutErr := ValidateApiRate(SourceRestClient, "core")
	if timeoutErr != nil {
		OutputError(timeoutErr.Error(), true)
	}

	// get number of secrets
	secretCount := 0
	var secretsResponse secrets
	secretsErr := SourceRestClient.Get(
		fmt.Sprintf(
			"repos/%s/actions/secrets",
			repoToProcess.NameWithOwner,
		),
		&secretsResponse,
	)
	Debug(fmt.Sprintf(
		"Secrets from %s: %v",
		repoToProcess.NameWithOwner,
		secretsResponse,
	))
	if secretsErr != nil {
		ExitManual(secretsErr)
	} else {
		secretCount = len(secretsResponse.Secrets)
		repoToProcess.Secrets = secretCount
	}

	// validate we have API attempts left
	timeoutErr = ValidateApiRate(SourceRestClient, "core")
	if timeoutErr != nil {
		OutputError(timeoutErr.Error(), true)
	}

	// get number of variables
	variableCount := 0
	var variablesResponse variables
	variablesErr := SourceRestClient.Get(
		fmt.Sprintf(
			"repos/%s/actions/variables",
			repoToProcess.NameWithOwner,
		),
		&variablesResponse,
	)
	Debug(fmt.Sprintf(
		"Variables from %s: %v",
		repoToProcess.NameWithOwner,
		variablesResponse,
	))
	if variablesErr != nil {
		ExitManual(variablesErr)
	} else {
		variableCount = len(variablesResponse.Variables)
		repoToProcess.Variables = variableCount
	}

	// validate we have API attempts left
	timeoutErr = ValidateApiRate(SourceRestClient, "core")
	if timeoutErr != nil {
		OutputError(timeoutErr.Error(), true)
	}

	// get number of variables
	envCount := 0
	var envResponse environments
	envsErr := SourceRestClient.Get(
		fmt.Sprintf(
			"repos/%s/environments",
			repoToProcess.NameWithOwner,
		),
		&envResponse,
	)
	Debug(fmt.Sprintf(
		"Environments from %s: %v",
		repoToProcess.NameWithOwner,
		envResponse,
	))
	if envsErr != nil {
		ExitManual(envsErr)
	} else {
		envCount = len(envResponse.Environments)
		repoToProcess.Environments = envCount
	}

	// write to table for output
	ResultsTable = append(ResultsTable, []string{
		repoToProcess.NameWithOwner,
		fmt.Sprintf("%d", secretCount),
		fmt.Sprintf("%d", variableCount),
		fmt.Sprintf("%d", envCount),
	})

	// find index of repo in original list and overwite it
	idx := slices.IndexFunc(Repositories, func(r repository) bool {
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
		Repositories[idx] = repoToProcess
	}

	// sleep for a second to avoid rate limiting
	time.Sleep(time.Duration(1))

	// close out this thread
	WaitGroup.Done()
}
