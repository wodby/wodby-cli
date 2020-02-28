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
				appServiceBuildConfigs {
					name
					title
					slug
					managed
					main
					image
					dockerfile
				}
			}
		}
	}`

	DOCKER_REGISTRY_CREDENTIALS = `
	query dockerRegistryCredentials($appBuildID: Int!) {
		dockerRegistryCredentials(appBuildID: $appBuildID) {
			host
			username
			password
		}
	}`

	DEPLOY = `
	query deploy($input: DeploymentInput!) {
		deploy(input: $input)
	}`
)
