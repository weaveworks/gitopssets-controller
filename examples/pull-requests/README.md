# Using the PullRequests Generator example

You will need an API key that can query the GitHub API, it does not need write access to your repositories.

**WARNING:** API Access Tokens can be used to impersonate users, they have no 2FA elements associated, and as such, they are a significant security risk.

Always restrict who can access a token.

Go to https://github.com/settings/tokens

And select _Generate New token_

<img width="204" alt="Generate new token (Beta" src="https://user-images.githubusercontent.com/867746/219933982-d1552bff-8361-4c7e-8c00-0a1b65fb1f13.png">

Select Repository access

<img width="521" alt="Repository access" src="https://user-images.githubusercontent.com/867746/219933993-edb1fcbd-a2f6-4c3a-beb8-5e10294a466c.png">

Choose the Repositories you want to control access to, this includes all public repos.

Configure “Read-only” access to Pull requests

<img width="808" alt="Screenshot 2023-02-19 at 06 33 40" src="https://user-images.githubusercontent.com/867746/219934021-4ec7308f-c544-4bf9-a6f1-880143d33dba.png">

Then go to generate a token

<img width="802" alt="Overview" src="https://user-images.githubusercontent.com/867746/219934035-f371479d-a0ae-4860-b36e-3063be8797e3.png">
The two permissions are the Pull requests access which was added, and the mandatory Metadata access

```shell
$ kubectl create secret generic github-secret --from-literal=secret=<insert secret>
```

Then apply the following YAML...

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: pull-request-generator-sa
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pull-request-generator-secret-role
  namespace: default
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "watch", "list"]
  resourceNames: ["github-secret"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pull-request-generator-secret-binding
  namespace: default
subjects:
- kind: ServiceAccount
  name: pull-request-generator-sa
  namespace: default
roleRef:
  kind: Role
  name: pull-request-generator-secret-role
  apiGroup: rbac.authorization.k8s.io
```

This will authenticate access to the GitHub API.

You can verify the RBAC access...

```shell
$ kubectl auth can-i get secret/github-secret -n default --as=system:serviceaccount:default:pull-request-generator-sa
yes
kubectl auth can-i update secret/github-secret -n default --as=system:serviceaccount:default:pull-request-generator-sa
no
```
**NOTE:** Do not allow other users to access this secret.

The RBAC configuration above allows the pull request service account to access the secret, but this does **not** prevent anybody else from being granted access.
