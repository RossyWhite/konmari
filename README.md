# konmari

`konmari` is a garbage collector to delete `ConfigMaps` and `Secrets` that are no longer referenced by Pods.

## Running as a CronJob

### Setup RBAC

```bash
$ kuectl create -f kubernetes/rbac.yaml
```

### Create a CronJob

```bash
$ kuectl create -f kubernetes/cronjob.yaml
```

## Command line flags

| Flag | Description | Default |
| :--- | :--- | :--- |
| `namespace` | Namespace in which konmari run. | `default` |
| `deletePeriod` | Period to judge as old Object. | `24*time.Hour*30` |
| `kubeconfig` | Path to kubeconfig file with authorization and master location information. | `""` |
| `dryrun` | Whether or not to actually delete Objects. | `false` |
| `disableSecrets` | Whether or not to ignore `Secrets`. | `false` |
| `disableConfigMaps` | Whether or not to ignore `ConfigMaps`. | `false` |
