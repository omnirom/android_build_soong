# Select statements

## Introduction

Soong currently has the arch, target, product_variables, and soong config variable properties that all support changing the values of soong properties based on some condition. These are confusing for users, and particularly the soong config variable properties require a lot of boilerplate that is annoying to write.

In addition, these properties are all implemented using reflection on property structs, and can combine in ways that the original authors did not expect. For example, soong config variables can be combined with arch/target by saying that they operate on "arch.arm.enabled" instead of just "enabled". But product variables do not have the same abilities.

The goal here is to combine all these different configuration mechanisms into one, to reduce complexity and boilerplate both in Android.bp files and in soong code.

## Usage

The soong select statements take their name and inspiration from [bazel select statements](https://bazel.build/docs/configurable-attributes).

### Syntax

#### Basic

The basic syntax for select statements looks like:

```
my_module_type {
  name: "my_module",
  some_string_property: select(arch(), {
    "arm": "foo",
    "x86": "bar",
    default: "baz",
  }),
}
```

That is, `select(` followed by a variable function, then a map of values of the variable to values to set the property to.

Arguments can be passed to the "functions" that look up axes:

```
select(soong_config_variable("my_namespace", "my_variable"), {
  "value1": "foo",
  default: "bar",
})
```


The list of functions that can currently be selected on:
 - `arch()`
 - `os()`
 - `soong_config_variable(namespace, variable)`
 - `release_flag(flag)`

The functions are [defined here](https://cs.android.com/android/platform/superproject/main/+/main:build/soong/android/module.go;l=2144;drc=3f01580c04bfe37c920e247015cce93cff2451c0), and it should be easy to add more.

#### Multivariable

To handle multivariable selects, multiple axes can be specified within parenthesis, to look like tuple destructuring. All of the variables being selected must match the corresponding value from the branch in order for the branch to be chosen.

```
select((arch(), os()), {
  ("arm", "linux"): "foo",
  (default, "windows"): "bar",
  (default, default): "baz",
})
```

#### Unset

You can have unset branches of selects using the "unset" keyword, which will act as if the property was not assigned to. This is only really useful if you’re using defaults modules.

```
cc_binary {
  name: "my_binary",
  enabled: select(os(), {
    "darwin": false,
    default: unset,
  }),
}
```

#### Appending

You can append select statements to both scalar values and other select statements:

```
my_module_type {
  name: "my_module",
  // string_property will be set to something like penguin-four, apple-two, etc.
  string_property: select(os(), {
    "linux_glibc": "penguin",
    "darwin": "apple",
    default: "unknown",
  }) + "-" + select(soong_config_variable("ANDROID", "favorite_vehicle"), {
    "car": "four",
    "tricycle": "three",
    "bike": "two",
     default: "unknown",
  })
}
```


You can also append a select with a value with another select that may not have a value, because some of its branches are "unset". If an unset branch was selected it will not append anything to the other select.

#### Binding the selected value to a Blueprint variable and the "any" keyword

In case you want to allow a selected value to have an unbounded number of possible values, you can bind its value to a blueprint variable and use it within the expression for that select branch.

```
my_module_type {
  name: "my_module",
  my_string_property: select(soong_config_variable("my_namespace", "my_variable"), {
    "some_value": "baz",
    any @ my_var: "foo" + my_var,
    default: "bar",
  }),
}
```

The syntax is `any @ my_variable_name: <expression using my_variable_name>`. `any` is currently the only pattern that can be bound to a variable, but we may add more in the future. `any` is equivalent to `default` except it will not match undefined select conditions.

#### Errors

If a select statement does not have a "default" branch, and none of the other branches match the variable being selected on, it’s a compile-time error. This may be useful for enforcing a variable is 1 of only a few values.

```
# in product config:
$(call soong_config_set,ANDROID,my_variable,foo)

// in an Android.bp:
my_module_type {
  name: "my_module",
  // Will error out with: soong_config_variable("ANDROID", "my_variable") had value "foo", which was not handled by the select
  enabled: select(soong_config_variable("ANDROID", "my_variable"), {
    "bar": true,
    "baz": false,
  }),
}
```

### Changes to property structs to support selects

Currently, the way configurable properties work is that there is secretly another property struct that has the `target`, `arch`, etc. properties, and then when the arch mutator (or other relevant mutator) runs, it copies the values of these properties onto the regular property structs. There’s nothing stopping you from accessing your properties from a mutator that runs before the one that updates the properties based on configurable values. This is a potential source of bugs, and we want to make sure that select statements don’t have the same pitfall. For that reason, you have to read property’s values through a getter which can do this check. This requires changing the code on a property-by-property basis to support selects.

To make a property support selects, it must be of type [proptools.Configurable[T]](https://cs.android.com/android/platform/superproject/main/+/main:build/blueprint/proptools/configurable.go;l=341;drc=a52b058cccd2caa778d0f97077adcd4ef7ffb68a). T is the old type of the property. Currently we support bool, string, and []string. Configurable has a `Get(evaluator)` method to get the value of the property. The evaluator can be a ModuleContext, or if you’re in a situation where you only have a very limited context and a module, (such as in a singleton) you can use [ModuleBase.ConfigurableEvaluator](https://cs.android.com/android/platform/superproject/main/+/main:build/soong/android/module.go;l=2133;drc=e19f741052cce097da940d9083d3f29e668de5cb).

`proptools.Configurable[T]` will handle unset properties for you, so you don’t need to make it a pointer type. However, there is a not-widely-known feature of property structs, where normally, properties are appended when squashing defaults. But if the property was a pointer property, later defaults replace earlier values instead of appending. With selects, to maintain this behavior, add the `android:"replace_instead_of_append"` struct tag. The "append" behavior for boolean values is to boolean OR them together, which is rarely what you want, so most boolean properties are pointers today.

Old:
```
type commonProperties struct {
  Enabled *bool `android:"arch_variant"`
}

func (m *ModuleBase) Enabled() *bool {
  return m.commonProperties.Enabled
}
```

New:
```
type commonProperties struct {
  Enabled proptools.Configurable[bool] `android:"arch_variant,replace_instead_of_append"`
}

func (m *ModuleBase) Enabled(ctx ConfigAndErrorContext) *bool {
  return m.commonProperties.Enabled.Get(m.ConfigurableEvaluator(ctx))
}
```

The `android:"arch_variant"` tag is kept to support the old `target:` and `arch:` properties with this property, but if all their usages in bp files were replaced by selects, then that tag could be removed.

The enabled property underwent this migration in https://r.android.com/3066188
