package server

import "goodsentry/internal/store"

// The compatible CLI consumes deployment projects as slug strings. Native
// pages retain project metadata for the embedded UI.
type deploymentResponse struct {
	store.ReleaseDeploy
	Projects []string `json:"projects"`
}

type releaseResponse struct {
	store.Release
	LastDeploy *deploymentResponse `json:"lastDeploy"`
}

func deploymentJSON(value store.ReleaseDeploy) deploymentResponse {
	result := deploymentResponse{ReleaseDeploy: value, Projects: make([]string, 0, len(value.Projects))}
	for _, project := range value.Projects {
		result.Projects = append(result.Projects, project.Slug)
	}
	return result
}

func releaseJSON(value store.Release) releaseResponse {
	result := releaseResponse{Release: value}
	if value.LastDeploy != nil {
		deploy := deploymentJSON(*value.LastDeploy)
		result.LastDeploy = &deploy
	}
	return result
}
