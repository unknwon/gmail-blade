package main

import (
	"context"
	"regexp"
	"slices"
	"strconv"

	"github.com/emersion/go-imap/v2"
	"github.com/google/go-github/v73/github"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

// githubPR represents a parsed GitHub pull request from an email notification.
type githubPR struct {
	Owner  string
	Repo   string
	Number int
}

// GitHub pull request URLs follow the pattern: https://github.com/owner/repo/pull/123
var githubPullRequestURLRegex = regexp.MustCompile(`https://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

// parseGitHubNotification extracts GitHub PR information from email content.
func parseGitHubNotification(body string) (*githubPR, error) {
	// Try to find PR URL in body
	matches := githubPullRequestURLRegex.FindStringSubmatch(body)
	if len(matches) < 4 {
		return nil, errors.New("could not find GitHub pull request URL in email")
	}

	owner := matches[1]
	repo := matches[2]
	prNumber, err := strconv.Atoi(matches[3])
	if err != nil {
		return nil, errors.Wrapf(err, "parse pull request number %q", matches[3])
	}

	return &githubPR{
		Owner:  owner,
		Repo:   repo,
		Number: prNumber,
	}, nil
}

// processGitHubReview handles the "github review" action.
func processGitHubReview(logger Logger, ctx context.Context, config configGitHub, uid imap.UID, body string) error {
	parsedPullRequest, err := parseGitHubNotification(body)
	if err != nil {
		return errors.Wrap(err, "parse GitHub notification")
	}

	repoFullName := parsedPullRequest.Owner + "/" + parsedPullRequest.Repo
	logger.Debug("Found GitHub pull request", "uid", uid, "repo", repoFullName, "pr", parsedPullRequest.Number)

	// Check if repository is allowed
	if !slices.Contains(config.Approval.AllowedRepositories, repoFullName) {
		logger.Warn("Repository not in allowed list", "uid", uid, "repo", repoFullName, "allowed", config.Approval.AllowedRepositories)
		return nil
	}

	// Create GitHub client
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.PersonalAccessToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Get pull request author
	pullRequest, _, err := client.PullRequests.Get(ctx, parsedPullRequest.Owner, parsedPullRequest.Repo, parsedPullRequest.Number)
	if err != nil {
		return errors.Wrapf(err, "get GitHub pull request %s#%d", repoFullName, parsedPullRequest.Number)
	}

	author := pullRequest.GetUser().GetLogin()
	logger.Debug("Found pull request author", "uid", uid, "author", author)

	// Check if author is allowed
	if !slices.Contains(config.Approval.AllowedUsernames, author) {
		logger.Warn("Author not in allowed list", "uid", uid, "author", author, "allowed", config.Approval.AllowedUsernames)
		return nil
	}

	// Check if already approved by current user
	reviews, _, err := client.PullRequests.ListReviews(ctx, parsedPullRequest.Owner, parsedPullRequest.Repo, parsedPullRequest.Number, nil)
	if err != nil {
		return errors.Wrapf(err, "list reviews for GitHub pull request %s#%d", repoFullName, parsedPullRequest.Number)
	}

	// Get current user to check for existing approval
	currentUser, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return errors.Wrap(err, "get current user")
	}

	for _, review := range reviews {
		if review.GetUser().GetLogin() == currentUser.GetLogin() && review.GetState() == "APPROVED" {
			logger.Debug("Already approved GitHub pull request", "uid", uid, "repo", repoFullName, "pr", parsedPullRequest.Number)
			return nil
		}
	}

	// Submit review approval
	review := &github.PullRequestReviewRequest{
		Event: github.Ptr("APPROVE"),
	}

	_, _, err = client.PullRequests.CreateReview(ctx, parsedPullRequest.Owner, parsedPullRequest.Repo, parsedPullRequest.Number, review)
	if err != nil {
		return errors.Wrapf(err, "approve GitHub pull request %s#%d", repoFullName, parsedPullRequest.Number)
	}

	logger.Info("Successfully approved GitHub pull request", "uid", uid, "repo", repoFullName, "pr", parsedPullRequest.Number)
	return nil
}
