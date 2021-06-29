// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types2

import "cmd/compile/internal/syntax"

func (check *Checker) interfaceType(ityp *Interface, iface *syntax.InterfaceType, def *Named) {
	var tlist []syntax.Expr // types collected from all type lists
	var tname *syntax.Name  // most recent "type" name

	addEmbedded := func(pos syntax.Pos, typ Type) {
		ityp.embeddeds = append(ityp.embeddeds, typ)
		if ityp.embedPos == nil {
			ityp.embedPos = new([]syntax.Pos)
		}
		*ityp.embedPos = append(*ityp.embedPos, pos)
	}

	for _, f := range iface.MethodList {
		if f.Name == nil {
			// We have an embedded type; possibly a union of types.
			addEmbedded(f.Type.Pos(), parseUnion(check, flattenUnion(nil, f.Type)))
			continue
		}
		// f.Name != nil

		// We have a method with name f.Name, or a type of a type list (f.Name.Value == "type").
		name := f.Name.Value
		if name == "_" {
			if check.conf.CompilerErrorMessages {
				check.error(f.Name, "methods must have a unique non-blank name")
			} else {
				check.error(f.Name, "invalid method name _")
			}
			continue // ignore
		}

		// TODO(gri) Remove type list handling once the parser doesn't accept type lists anymore.
		if name == "type" {
			// Report an error for the first type list per interface
			// if we don't allow type lists, but continue.
			if !check.conf.AllowTypeLists && tlist == nil {
				check.softErrorf(f.Name, "use generalized embedding syntax instead of a type list")
			}
			// For now, collect all type list entries as if it
			// were a single union, where each union element is
			// of the form ~T.
			op := new(syntax.Operation)
			// We should also set the position (but there is no setter);
			// we don't care because this code will eventually go away.
			op.Op = syntax.Tilde
			op.X = f.Type
			tlist = append(tlist, op)
			// Report an error if we have multiple type lists in an
			// interface, but only if they are permitted in the first place.
			if check.conf.AllowTypeLists && tname != nil && tname != f.Name {
				check.error(f.Name, "cannot have multiple type lists in an interface")
			}
			tname = f.Name
			continue
		}

		typ := check.typ(f.Type)
		sig, _ := typ.(*Signature)
		if sig == nil {
			if typ != Typ[Invalid] {
				check.errorf(f.Type, invalidAST+"%s is not a method signature", typ)
			}
			continue // ignore
		}

		// Always type-check method type parameters but complain if they are not enabled.
		// (This extra check is needed here because interface method signatures don't have
		// a receiver specification.)
		if sig.tparams != nil && !acceptMethodTypeParams {
			check.error(f.Type, "methods cannot have type parameters")
		}

		// use named receiver type if available (for better error messages)
		var recvTyp Type = ityp
		if def != nil {
			recvTyp = def
		}
		sig.recv = NewVar(f.Name.Pos(), check.pkg, "", recvTyp)

		m := NewFunc(f.Name.Pos(), check.pkg, name, sig)
		check.recordDef(f.Name, m)
		ityp.methods = append(ityp.methods, m)
	}

	// If we saw a type list, add it like an embedded union.
	if tlist != nil {
		// Types T in a type list are added as ~T expressions but we don't
		// have the position of the '~'. Use the first type position instead.
		addEmbedded(tlist[0].(*syntax.Operation).X.Pos(), parseUnion(check, tlist))
	}

	// All methods and embedded elements for this interface are collected;
	// i.e., this interface is may be used in a type set computation.
	ityp.complete = true

	if len(ityp.methods) == 0 && len(ityp.embeddeds) == 0 {
		// empty interface
		ityp.tset = &topTypeSet
		return
	}

	// sort for API stability
	// (don't sort embeddeds: they must correspond to *embedPos entries)
	sortMethods(ityp.methods)

	// Compute type set with a non-nil *Checker as soon as possible
	// to report any errors. Subsequent uses of type sets should be
	// using this computed type set and won't need to pass in a *Checker.
	check.later(func() { newTypeSet(check, iface.Pos(), ityp) })
}

func flattenUnion(list []syntax.Expr, x syntax.Expr) []syntax.Expr {
	if o, _ := x.(*syntax.Operation); o != nil && o.Op == syntax.Or {
		list = flattenUnion(list, o.X)
		x = o.Y
	}
	return append(list, x)
}