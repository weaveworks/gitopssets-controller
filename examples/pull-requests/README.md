# Using the PullRequests Generator example

You will need an API key that can query the GitHub API.

Generate a GitHub Personal Access Token ([PAT](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/creating-a-personal-access-token)) and put into a secret.

```shell
$ kubectl create secret generic github-secret \
  --from-literal=secret=<insert GitHub access token here>
```

This will authenticate access to the GitHub API.
