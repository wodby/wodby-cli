package api

const (
	APP_BUILD = `
	query appBuild($id: Int!) {
		appBuild(id: $id) {
			id
			number
			gitRefType
			gitRef
			config {
				registryHost
				appServiceBuildConfigs {
					name
					title
					slug
					managed
					main
					image
					dockerfile
					dockerignore
				}
			}
		}
	}`

	DOCKER_REGISTRY_CREDENTIALS = `
	query dockerRegistryCredentials($appBuildID: Int!) {
		dockerRegistryCredentials(appBuildID: $appBuildID) {
			username
			password
		}
	}`

	DEPLOY = `
	mutation deployFromCI($input: DeploymentFromCIInput!) {
		deployFromCI(input: $input) {
			id
		}
	}`
)
