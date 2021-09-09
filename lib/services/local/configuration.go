/*
Copyright 2017 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package local

import (
	"context"

	"github.com/gravitational/trace"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/backend"
	"github.com/gravitational/teleport/lib/modules"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/prometheus/client_golang/prometheus"
)

var clusterNameNotFound = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: teleport.MetricClusterNameNotFound,
		Help: "Number of times a cluster name was not found",
	},
)

// ClusterConfigurationService is responsible for managing cluster configuration.
type ClusterConfigurationService struct {
	backend.Backend
}

// NewClusterConfigurationService returns a new ClusterConfigurationService.
func NewClusterConfigurationService(backend backend.Backend) (*ClusterConfigurationService, error) {
	err := utils.RegisterPrometheusCollectors(clusterNameNotFound)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &ClusterConfigurationService{
		Backend: backend,
	}, nil
}

// GetClusterName gets the name of the cluster from the backend.
func (s *ClusterConfigurationService) GetClusterName(opts ...services.MarshalOption) (types.ClusterName, error) {
	item, err := s.Get(context.TODO(), backend.Key(clusterConfigPrefix, namePrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			clusterNameNotFound.Inc()
			return nil, trace.NotFound("cluster name not found")
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalClusterName(item.Value,
		services.AddOptions(opts, services.WithResourceID(item.ID))...)
}

// DeleteClusterName deletes types.ClusterName from the backend.
func (s *ClusterConfigurationService) DeleteClusterName() error {
	err := s.Delete(context.TODO(), backend.Key(clusterConfigPrefix, namePrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("cluster configuration not found")
		}
		return trace.Wrap(err)
	}
	return nil
}

// SetClusterName sets the name of the cluster in the backend. SetClusterName
// can only be called once on a cluster after which it will return trace.AlreadyExists.
func (s *ClusterConfigurationService) SetClusterName(c types.ClusterName) error {
	// DELETE IN 8.0.0: Move this ClusterID check to ClusterName.CheckAndSetDefaults.
	if c.GetClusterID() == "" {
		return trace.BadParameter("cluster ID is required")
	}
	return s.ForceSetClusterName(c)
}

// ForceSetClusterName creates types.ClusterName on the backend
// without additional field checks.  To be used only in tests.
// DELETE IN 8.0.0
func (s *ClusterConfigurationService) ForceSetClusterName(c types.ClusterName) error {
	value, err := services.MarshalClusterName(c)
	if err != nil {
		return trace.Wrap(err)
	}

	_, err = s.Create(context.TODO(), backend.Item{
		Key:     backend.Key(clusterConfigPrefix, namePrefix),
		Value:   value,
		Expires: c.Expiry(),
	})
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

// UpsertClusterName sets the name of the cluster in the backend.
func (s *ClusterConfigurationService) UpsertClusterName(c types.ClusterName) error {
	// DELETE IN 8.0.0: Move this ClusterID check to ClusterName.CheckAndSetDefaults.
	if c.GetClusterID() == "" {
		return trace.BadParameter("cluster ID is required")
	}

	value, err := services.MarshalClusterName(c)
	if err != nil {
		return trace.Wrap(err)
	}

	_, err = s.Put(context.TODO(), backend.Item{
		Key:     backend.Key(clusterConfigPrefix, namePrefix),
		Value:   value,
		Expires: c.Expiry(),
		ID:      c.GetResourceID(),
	})
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

// GetStaticTokens gets the list of static tokens used to provision nodes.
func (s *ClusterConfigurationService) GetStaticTokens() (types.StaticTokens, error) {
	item, err := s.Get(context.TODO(), backend.Key(clusterConfigPrefix, staticTokensPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("static tokens not found")
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalStaticTokens(item.Value,
		services.WithResourceID(item.ID), services.WithExpires(item.Expires))
}

// SetStaticTokens sets the list of static tokens used to provision nodes.
func (s *ClusterConfigurationService) SetStaticTokens(c types.StaticTokens) error {
	value, err := services.MarshalStaticTokens(c)
	if err != nil {
		return trace.Wrap(err)
	}
	_, err = s.Put(context.TODO(), backend.Item{
		Key:     backend.Key(clusterConfigPrefix, staticTokensPrefix),
		Value:   value,
		Expires: c.Expiry(),
		ID:      c.GetResourceID(),
	})
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

// DeleteStaticTokens deletes static tokens
func (s *ClusterConfigurationService) DeleteStaticTokens() error {
	err := s.Delete(context.TODO(), backend.Key(clusterConfigPrefix, staticTokensPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("static tokens are not found")
		}
		return trace.Wrap(err)
	}
	return nil
}

// GetAuthPreference fetches the cluster authentication preferences
// from the backend and return them.
func (s *ClusterConfigurationService) GetAuthPreference(ctx context.Context) (types.AuthPreference, error) {
	item, err := s.Get(ctx, backend.Key(authPrefix, preferencePrefix, generalPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("authentication preference not found")
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalAuthPreference(item.Value,
		services.WithResourceID(item.ID), services.WithExpires(item.Expires))
}

// SetAuthPreference sets the cluster authentication preferences
// on the backend.
func (s *ClusterConfigurationService) SetAuthPreference(ctx context.Context, preferences types.AuthPreference) error {
	// Perform the modules-provided checks.
	if err := modules.ValidateResource(preferences); err != nil {
		return trace.Wrap(err)
	}

	value, err := services.MarshalAuthPreference(preferences)
	if err != nil {
		return trace.Wrap(err)
	}

	item := backend.Item{
		Key:   backend.Key(authPrefix, preferencePrefix, generalPrefix),
		Value: value,
		ID:    preferences.GetResourceID(),
	}

	_, err = s.Put(ctx, item)
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

// DeleteAuthPreference deletes types.AuthPreference from the backend.
func (s *ClusterConfigurationService) DeleteAuthPreference(ctx context.Context) error {
	err := s.Delete(ctx, backend.Key(authPrefix, preferencePrefix, generalPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("auth preference not found")
		}
		return trace.Wrap(err)
	}
	return nil
}

// GetClusterConfig gets types.ClusterConfig from the backend.
func (s *ClusterConfigurationService) GetClusterConfig(opts ...services.MarshalOption) (types.ClusterConfig, error) {
	ctx := context.TODO()

	var clusterConfig types.ClusterConfig
	item, err := s.Get(ctx, backend.Key(clusterConfigPrefix, generalPrefix))
	if err != nil {
		if !trace.IsNotFound(err) {
			return nil, trace.Wrap(err)
		}
		// When there is no legacy ClusterConfig stored in the backend, supply
		// a default ClusterConfig instead (to be filled with data from the other
		// resources).  This helps keep backward compatibility when a non-upgraded
		// v7.x auth server needs to work with v6.x cluster components.
		clusterConfig = types.DefaultClusterConfig()
	} else {
		clusterConfig, err = services.UnmarshalClusterConfig(item.Value, append(opts, services.WithResourceID(item.ID), services.WithExpires(item.Expires))...)
		if err != nil {
			return nil, trace.Wrap(err)
		}
	}

	// To ensure backward compatibility, extend the fetched ClusterConfig
	// resource with the ID that is now stored in ClusterName.
	// (But only if the cluster ID is not set already, to retain the ability
	// to provide legacy cluster ID.)
	// DELETE IN 8.0.0
	if clusterConfig.GetLegacyClusterID() == "" {
		clusterName, err := s.GetClusterName()
		if err != nil {
			return nil, trace.Wrap(err)
		}
		clusterConfig.SetLegacyClusterID(clusterName.GetClusterID())
	}

	// To ensure backward compatibility, extend the fetched ClusterConfig
	// resource with the values that are now stored in ClusterAuditConfig.
	// DELETE IN 8.0.0
	auditConfig, err := s.GetClusterAuditConfig(context.TODO())
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if err := clusterConfig.SetAuditConfig(auditConfig); err != nil {
		return nil, trace.Wrap(err)
	}

	// To ensure backward compatibility, extend the fetched ClusterConfig
	// resource with the values that are now stored in ClusterNetworkingConfig.
	// DELETE IN 8.0.0
	netConfig, err := s.GetClusterNetworkingConfig(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if err := clusterConfig.SetNetworkingFields(netConfig); err != nil {
		return nil, trace.Wrap(err)
	}

	// To ensure backward compatibility, extend the fetched ClusterConfig
	// resource with the values that are now stored in SessionRecordingConfig.
	// DELETE IN 8.0.0
	recConfig, err := s.GetSessionRecordingConfig(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if err := clusterConfig.SetSessionRecordingFields(recConfig); err != nil {
		return nil, trace.Wrap(err)
	}

	// To ensure backward compatibility, extend the fetched ClusterConfig
	// resource with the values that are now stored in AuthPreference.
	// DELETE IN 8.0.0
	authPref, err := s.GetAuthPreference(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if err := clusterConfig.SetAuthFields(authPref); err != nil {
		return nil, trace.Wrap(err)
	}

	return clusterConfig, nil
}

// DeleteClusterConfig deletes types.ClusterConfig from the backend.
func (s *ClusterConfigurationService) DeleteClusterConfig() error {
	err := s.Delete(context.TODO(), backend.Key(clusterConfigPrefix, generalPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("cluster configuration not found")
		}
		return trace.Wrap(err)
	}
	return nil
}

// SetClusterConfig sets types.ClusterConfig on the backend.
func (s *ClusterConfigurationService) SetClusterConfig(c types.ClusterConfig) error {
	if c.HasAuditConfig() {
		return trace.BadParameter("cluster config has legacy audit config, call SetClusterAuditConfig to set these fields")
	}
	if c.HasNetworkingFields() {
		return trace.BadParameter("cluster config has legacy networking fields, call SetClusterNetworkingConfig to set these fields")
	}
	if c.HasSessionRecordingFields() {
		return trace.BadParameter("cluster config has legacy session recording fields, call SetSessionRecordingConfig to set these fields")
	}
	if c.HasAuthFields() {
		return trace.BadParameter("cluster config has legacy auth fields, call SetAuthPreference to set these fields")
	}
	if c.GetLegacyClusterID() != "" {
		return trace.BadParameter("cluster config has legacy cluster ID set, call SetClusterName to set this field")
	}

	return s.ForceSetClusterConfig(c)
}

// ForceSetClusterConfig sets types.ClusterConfig on the backend
// without legacy field checks.  To be used only in tests.
func (s *ClusterConfigurationService) ForceSetClusterConfig(c types.ClusterConfig) error {
	value, err := services.MarshalClusterConfig(c)
	if err != nil {
		return trace.Wrap(err)
	}

	item := backend.Item{
		Key:   backend.Key(clusterConfigPrefix, generalPrefix),
		Value: value,
		ID:    c.GetResourceID(),
	}

	_, err = s.Put(context.TODO(), item)
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

// GetClusterAuditConfig gets cluster audit config from the backend.
func (s *ClusterConfigurationService) GetClusterAuditConfig(ctx context.Context, opts ...services.MarshalOption) (types.ClusterAuditConfig, error) {
	item, err := s.Get(ctx, backend.Key(clusterConfigPrefix, auditPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("cluster audit config not found")
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalClusterAuditConfig(item.Value, append(opts, services.WithResourceID(item.ID), services.WithExpires(item.Expires))...)
}

// SetClusterAuditConfig sets the cluster audit config on the backend.
func (s *ClusterConfigurationService) SetClusterAuditConfig(ctx context.Context, auditConfig types.ClusterAuditConfig) error {
	value, err := services.MarshalClusterAuditConfig(auditConfig)
	if err != nil {
		return trace.Wrap(err)
	}

	item := backend.Item{
		Key:   backend.Key(clusterConfigPrefix, auditPrefix),
		Value: value,
		ID:    auditConfig.GetResourceID(),
	}

	_, err = s.Put(ctx, item)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

// DeleteClusterAuditConfig deletes ClusterAuditConfig from the backend.
func (s *ClusterConfigurationService) DeleteClusterAuditConfig(ctx context.Context) error {
	err := s.Delete(ctx, backend.Key(clusterConfigPrefix, auditPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("cluster audit config not found")
		}
		return trace.Wrap(err)
	}
	return nil
}

// GetClusterNetworkingConfig gets cluster networking config from the backend.
func (s *ClusterConfigurationService) GetClusterNetworkingConfig(ctx context.Context, opts ...services.MarshalOption) (types.ClusterNetworkingConfig, error) {
	item, err := s.Get(ctx, backend.Key(clusterConfigPrefix, networkingPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("cluster networking config not found")
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalClusterNetworkingConfig(item.Value, append(opts, services.WithResourceID(item.ID), services.WithExpires(item.Expires))...)
}

// SetClusterNetworkingConfig sets the cluster networking config
// on the backend.
func (s *ClusterConfigurationService) SetClusterNetworkingConfig(ctx context.Context, netConfig types.ClusterNetworkingConfig) error {
	// Perform the modules-provided checks.
	if err := modules.ValidateResource(netConfig); err != nil {
		return trace.Wrap(err)
	}

	value, err := services.MarshalClusterNetworkingConfig(netConfig)
	if err != nil {
		return trace.Wrap(err)
	}

	item := backend.Item{
		Key:   backend.Key(clusterConfigPrefix, networkingPrefix),
		Value: value,
		ID:    netConfig.GetResourceID(),
	}

	_, err = s.Put(ctx, item)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

// DeleteClusterNetworkingConfig deletes ClusterNetworkingConfig from the backend.
func (s *ClusterConfigurationService) DeleteClusterNetworkingConfig(ctx context.Context) error {
	err := s.Delete(ctx, backend.Key(clusterConfigPrefix, networkingPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("cluster networking config not found")
		}
		return trace.Wrap(err)
	}
	return nil
}

// GetSessionRecordingConfig gets session recording config from the backend.
func (s *ClusterConfigurationService) GetSessionRecordingConfig(ctx context.Context, opts ...services.MarshalOption) (types.SessionRecordingConfig, error) {
	item, err := s.Get(ctx, backend.Key(clusterConfigPrefix, sessionRecordingPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("session recording config not found")
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalSessionRecordingConfig(item.Value, append(opts, services.WithResourceID(item.ID), services.WithExpires(item.Expires))...)
}

// SetSessionRecordingConfig sets session recording config on the backend.
func (s *ClusterConfigurationService) SetSessionRecordingConfig(ctx context.Context, recConfig types.SessionRecordingConfig) error {
	// Perform the modules-provided checks.
	if err := modules.ValidateResource(recConfig); err != nil {
		return trace.Wrap(err)
	}

	value, err := services.MarshalSessionRecordingConfig(recConfig)
	if err != nil {
		return trace.Wrap(err)
	}

	item := backend.Item{
		Key:   backend.Key(clusterConfigPrefix, sessionRecordingPrefix),
		Value: value,
		ID:    recConfig.GetResourceID(),
	}

	_, err = s.Put(ctx, item)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

// DeleteSessionRecordingConfig deletes SessionRecordingConfig from the backend.
func (s *ClusterConfigurationService) DeleteSessionRecordingConfig(ctx context.Context) error {
	err := s.Delete(ctx, backend.Key(clusterConfigPrefix, sessionRecordingPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("session recording config not found")
		}
		return trace.Wrap(err)
	}
	return nil
}

func (s *ClusterConfigurationService) SetClusterEncryptionConfig(ctx context.Context, recConfig types.ClusterEncryptionConfig) error {
	// Perform the modules-provided checks.
	if err := modules.ValidateResource(recConfig); err != nil {
		return trace.Wrap(err)
	}

	value, err := services.MarshalEncryptionConfig(recConfig)
	if err != nil {
		return trace.Wrap(err)
	}

	item := backend.Item{
		Key:   backend.Key(clusterConfigPrefix, encryptionPrefix),
		Value: value,
		ID:    recConfig.GetResourceID(),
	}

	_, err = s.Put(ctx, item)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func (s *ClusterConfigurationService) GetClusterEncryptionConfig(ctx context.Context, opts ...services.MarshalOption) (types.ClusterEncryptionConfig, error) {
	item, err := s.Get(ctx, backend.Key(clusterConfigPrefix, encryptionPrefix))
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("encryption config not found")
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalEncryptionConfig(item.Value, append(opts, services.WithResourceID(item.ID), services.WithExpires(item.Expires))...)
}

const (
	clusterConfigPrefix    = "cluster_configuration"
	namePrefix             = "name"
	staticTokensPrefix     = "static_tokens"
	authPrefix             = "authentication"
	preferencePrefix       = "preference"
	generalPrefix          = "general"
	auditPrefix            = "audit"
	networkingPrefix       = "networking"
	sessionRecordingPrefix = "session_recording"
	encryptionPrefix       =  "encryption"
)
