package main

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/emersion/go-imap/v2"
	"github.com/google/go-github/v73/github"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

// githubPullRequest contains GitHub pull request of an email notification.
type githubPullRequest struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Author string `json:"author"`
}

func (pr *githubPullRequest) Env() map[string]any {
	return map[string]any{
		"owner":  pr.Owner,
		"repo":   pr.Repo,
		"number": pr.Number,
		"author": pr.Author,
	}
}

// GitHub pull request URLs follow the pattern: https://github.com/owner/repo/pull/123
var githubPullRequestURLRegex = regexp.MustCompile(`https://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

// parseGitHubNotification extracts GitHub pull request information from email content.
func parseGitHubNotification(body string) (owner, repo string, number int, err error) {
	matches := githubPullRequestURLRegex.FindStringSubmatch(body)
	if len(matches) < 4 {
		return "", "", 0, errors.New("could not find GitHub pull request URL in email")
	}

	owner = matches[1]
	repo = matches[2]
	number, err = strconv.Atoi(matches[3])
	if err != nil {
		return "", "", 0, errors.Wrapf(err, "parse pull request number %q", matches[3])
	}
	return owner, repo, number, nil
}

func newGitHubClient(ctx context.Context, pat string) *github.Client {
	return github.NewClient(
		oauth2.NewClient(
			ctx,
			oauth2.StaticTokenSource(
				&oauth2.Token{AccessToken: pat},
			),
		),
	)
}

var githubPullRequestCache = make(map[string]*githubPullRequest)

// executePrefetchGitHubPullRequest fetches GitHub pull request data for prefetch.
func executePrefetchGitHubPullRequest(logger Logger, ctx context.Context, config configGitHub, body string) (*githubPullRequest, error) {
	owner, repo, number, err := parseGitHubNotification(body)
	if err != nil {
		return nil, errors.Wrap(err, "parse GitHub notification")
	}

	client := newGitHubClient(ctx, config.PersonalAccessToken)

	cacheKey := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	if pullRequest, ok := githubPullRequestCache[cacheKey]; ok {
		return pullRequest, nil
	}
	apiPullRequest, resp, err := client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, errors.Wrapf(err, "get GitHub pull request %s/%s#%d", owner, repo, number)
	}
	xRatelimitRemaining, _ := strconv.Atoi(resp.Header.Get("X-Ratelimit-Remaining"))
	if xRatelimitRemaining < 500 {
		logger.Warn("GitHub API rate limit quota is low", "remaining", xRatelimitRemaining)
	}

	author := apiPullRequest.GetUser().GetLogin()
	logger.Debug("Prefetched GitHub pull request data", "repo", owner+"/"+repo, "pr", number, "author", author)

	pullRequest := &githubPullRequest{
		Owner:  owner,
		Repo:   repo,
		Number: number,
		Author: author,
	}
	githubPullRequestCache[cacheKey] = pullRequest
	return pullRequest, nil
}

// processGitHubReview handles the "github review" action with prefetch data.
func processGitHubReview(logger Logger, ctx context.Context, config configGitHub, uid imap.UID, prefetchData map[string]enver) error {
	prData, ok := prefetchData[prefetchGitHubPullRequestKey].(*githubPullRequest)
	if !ok {
		return errors.New("invalid GitHub pull request prefetch data type")
	}

	repoFullName := prData.Owner + "/" + prData.Repo
	if !slices.Contains(config.Approval.AllowedRepositories, repoFullName) {
		logger.Debug("Repository not in allowed list", "uid", uid, "repo", repoFullName, "allowed", config.Approval.AllowedRepositories)
		return nil
	}

	if !slices.Contains(config.Approval.AllowedUsernames, prData.Author) {
		logger.Debug("Author not in allowed list", "uid", uid, "author", prData.Author, "allowed", config.Approval.AllowedUsernames)
		return nil
	}

	client := newGitHubClient(ctx, config.PersonalAccessToken)

	reviews, _, err := client.PullRequests.ListReviews(ctx, prData.Owner, prData.Repo, prData.Number, nil)
	if err != nil {
		return errors.Wrapf(err, "list reviews for GitHub pull request %s#%d", repoFullName, prData.Number)
	}

	currentUser, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return errors.Wrap(err, "get current user")
	}

	for _, review := range reviews {
		if review.GetUser().GetLogin() == currentUser.GetLogin() && review.GetState() == "APPROVED" {
			logger.Debug("Already approved GitHub pull request", "uid", uid, "repo", repoFullName, "pr", prData.Number)
			return nil
		}
	}

	review := &github.PullRequestReviewRequest{
		Event: github.Ptr("APPROVE"),
	}

	_, _, err = client.PullRequests.CreateReview(ctx, prData.Owner, prData.Repo, prData.Number, review)
	if err != nil {
		return errors.Wrapf(err, "approve GitHub pull request %s#%d", repoFullName, prData.Number)
	}

	logger.Info("Successfully approved GitHub pull request", "uid", uid, "repo", repoFullName, "pr", prData.Number)
	return nil
}
