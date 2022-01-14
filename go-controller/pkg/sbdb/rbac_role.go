// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package sbdb

import "github.com/ovn-org/libovsdb/model"

// RBACRole defines an object in RBAC_Role table
type RBACRole struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Permissions map[string]string `ovsdb:"permissions"`
}

func copyRBACRolePermissions(a map[string]string) map[string]string {
	if a == nil {
		return nil
	}
	b := make(map[string]string, len(a))
	for k, v := range a {
		b[k] = v
	}
	return b
}

func equalRBACRolePermissions(a, b map[string]string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}

func (a *RBACRole) DeepCopyInto(b *RBACRole) {
	*b = *a
	b.Permissions = copyRBACRolePermissions(a.Permissions)
}

func (a *RBACRole) DeepCopy() *RBACRole {
	b := new(RBACRole)
	a.DeepCopyInto(b)
	return b
}

func (a *RBACRole) CloneModelInto(b model.Model) {
	c := b.(*RBACRole)
	a.DeepCopyInto(c)
}

func (a *RBACRole) CloneModel() model.Model {
	return a.DeepCopy()
}

func (a *RBACRole) Equals(b *RBACRole) bool {
	return a.UUID == b.UUID &&
		a.Name == b.Name &&
		equalRBACRolePermissions(a.Permissions, b.Permissions)
}

func (a *RBACRole) EqualsModel(b model.Model) bool {
	c := b.(*RBACRole)
	return a.Equals(c)
}

var _ model.CloneableModel = &RBACRole{}
var _ model.ComparableModel = &RBACRole{}
