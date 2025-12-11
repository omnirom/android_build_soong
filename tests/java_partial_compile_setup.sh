#!/bin/bash -eu

set -o pipefail

function create_base_files {
  mkdir -p soong-test/java/integration/impllib
    cat > soong-test/java/integration/Android.bp <<'EOF'
java_library {
    name: "impl-library",
    srcs: [
        "impllib/**/*.java",
    ],
    libs: ["provider-library"],
    sdk_version: "35",
}

java_library {
    name: "provider-library",
    srcs: ["provider/**/*.java"],
    sdk_version: "35",
}
EOF

  cat > soong-test/java/integration/impllib/ExampleImpl1.java <<'EOF'
package soong.java.integration.impllib;

import soong.java.integration.provider.Provider;

class ExampleImpl1 {
  public static final int EXAMPLE_1_CONST_1 = 100;

  private Provider mProvider;

  public ExampleImpl1() {
    mProvider = new Provider();
  }

  public String getClassName() {
    return "ExampleInt1";
  }

  public int getProviderConstant() {
    return mProvider.nextProviderConst();
  }
}
EOF

  mkdir -p soong-test/java/integration/provider

  cat > soong-test/java/integration/provider/Provider.java <<'EOF'
package soong.java.integration.provider;

public class Provider {
  public static final int PROVIDER_CONST = -1;

  private String className;

  public Provider() {
    this.className = "Provider";
  }

  public int nextProviderConst() {
    return PROVIDER_CONST + 1;
  }

  private String getProviderName() {
    return className;
  }
}
EOF
}

function create_ap_files {
  mkdir -p soong-test/java/integration/impllib
      cat > soong-test/java/integration/Android.bp <<'EOF'
java_library {
    name: "impl-library",
    srcs: [
        "impllib/**/*.java",
        ":wrapper-annotations-source",
    ],
    libs: ["provider-library"],
    plugins: ["wrapper-annotation-processor"],
    sdk_version: "35",
}

java_library {
    name: "provider-library",
    srcs: ["provider/**/*.java"],
    sdk_version: "35",
}

java_library_host {
    name: "annotation-processor-lib",
    srcs: ["annotation/**/*.java"],
}

java_plugin {
    name: "wrapper-annotation-processor",
    processor_class: "soong.java.integration.annotation.WrapperProcessor",
    static_libs: ["annotation-processor-lib"],
    installable: false,
    generates_api: true,
}

filegroup {
    name: "wrapper-annotations-source",
    srcs: ["annotation/GenerateWrapper.java"],
}
EOF

  mkdir -p soong-test/java/integration/annotation
  cat > soong-test/java/integration/annotation/WrapperProcessor.java <<'EOF'
package soong.java.integration.annotation;
import soong.java.integration.annotation.GenerateWrapper;
import java.io.IOException;
import java.io.Writer;
import java.util.Set;
import javax.annotation.processing.AbstractProcessor;
import javax.annotation.processing.Filer;
import javax.annotation.processing.Messager;
import javax.annotation.processing.ProcessingEnvironment;
import javax.annotation.processing.Processor;
import javax.annotation.processing.RoundEnvironment;
import javax.annotation.processing.SupportedAnnotationTypes;
import javax.annotation.processing.SupportedSourceVersion;
import javax.lang.model.SourceVersion;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.util.Elements;
import javax.tools.Diagnostic;
import javax.tools.JavaFileObject;
@SupportedAnnotationTypes("soong.java.integration.annotation.GenerateWrapper")
@SupportedSourceVersion(SourceVersion.RELEASE_17)
public class WrapperProcessor extends AbstractProcessor {
  private Filer filer;
  private Messager messager;
  private Elements elementUtils;
  @Override
  public synchronized void init(ProcessingEnvironment processingEnv) {
    super.init(processingEnv);
    filer = processingEnv.getFiler();
    messager = processingEnv.getMessager();
    elementUtils = processingEnv.getElementUtils();
  }
  @Override
  public boolean process(Set<? extends TypeElement> annotations, RoundEnvironment roundEnv) {
    for (Element annotatedElement : roundEnv.getElementsAnnotatedWith(GenerateWrapper.class)) {
      ExecutableElement originalMethod = (ExecutableElement) annotatedElement;
      GenerateWrapper annotation = originalMethod.getAnnotation(GenerateWrapper.class);
      int methodCount = annotation.methodCount();
      TypeElement enclosingClass = (TypeElement) originalMethod.getEnclosingElement();
      String packageName = elementUtils.getPackageOf(enclosingClass).getQualifiedName().toString();
      String originalClassNameForNaming = enclosingClass.getSimpleName().toString();
      String wrapperClassName = originalClassNameForNaming + "Wrapper";
      String wrapperMethodName = originalMethod.getSimpleName().toString() + "Wrapper";
      try {
        generateAlwaysVoidNoParamWrapper(packageName, wrapperClassName, wrapperMethodName, methodCount);
      } catch (IOException e) {
        throw new RuntimeException("Unable to generate class using annotation");
      }
    }
    return true;
  }
  private void generateAlwaysVoidNoParamWrapper(String packageName, String wrapperClassName, String originalMethodNameForNaming, int methodCount) throws IOException {
    StringBuilder sb = new StringBuilder();
    sb.append("package ").append(packageName).append(";\n\n");
    sb.append("public final class ").append(wrapperClassName).append(" {\n\n");
    for (int i=0; i < methodCount; i++) {
      sb.append("    public String ").append(originalMethodNameForNaming + String.valueOf(i)).append("() {\n");
      sb.append("        return ").append("\"").append(wrapperClassName).append("\"").append(";\n");
      sb.append("    }\n");
    }
    sb.append("}\n");
    JavaFileObject jfo = filer.createSourceFile(packageName + "." + wrapperClassName);
    try (Writer writer = jfo.openWriter()) {
      writer.write(sb.toString());
    }
  }
}
EOF

  cat > soong-test/java/integration/annotation/GenerateWrapper.java <<'EOF'
package soong.java.integration.annotation;
import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;
@Retention(RetentionPolicy.SOURCE)
@Target(ElementType.METHOD)
public @interface GenerateWrapper {
  int methodCount() default 1;
}
EOF

  cat > soong-test/java/integration/impllib/AnnotationUsage.java <<'EOF'
package soong.java.integration.impllib;
import soong.java.integration.annotation.GenerateWrapper;
public class AnnotationUsage {
  @GenerateWrapper(methodCount = 2)
  public String getClassName() {
    return "AnnotationUsage";
  }
}
EOF

  cat > soong-test/java/integration/impllib/ExampleImpl4.java <<'EOF'
package soong.java.integration.impllib;
import soong.java.integration.impllib.AnnotationUsageWrapper;
public class ExampleImpl4 {
  private AnnotationUsageWrapper mAnnotationUsageWrapper;
  public ExampleImpl4() {
    this.mAnnotationUsageWrapper = new AnnotationUsageWrapper();
  }
  public String readGenApi() {
    return mAnnotationUsageWrapper.getClassNameWrapper1();
  }
}
EOF
}

create_example_impl3_file() {
  mkdir -p soong-test/java/integration/impllib

  cat > soong-test/java/integration/impllib/ExampleImpl3.java <<'EOF'
package soong.java.integration.impllib;

import soong.java.integration.impllib.ExampleImpl1;

public class ExampleImpl3 {

  public ExampleImpl3() {
  }

  public String getClassName() {
    return "ExampleImpl3";
  }

  private int getConstant() {
    return ExampleImpl1.EXAMPLE_1_CONST_1;
  }
}
EOF
}

modify_example_impl1_file() {
  mkdir -p soong-test/java/integration/impllib

  cat > soong-test/java/integration/impllib/ExampleImpl1.java <<'EOF'
package soong.java.integration.impllib;

import soong.java.integration.provider.Provider;

class ExampleImpl1 {
  public static final int EXAMPLE_1_CONST_1_CHANGED = 10000;

  private Provider mProvider;

  public ExampleImpl1() {
    mProvider = new Provider();
  }

  public String getClassName() {
    return "ExampleInt1";
  }

  public int getProviderConstant() {
    return mProvider.nextProviderConst();
  }
}
EOF
}

modify_provider_file() {
  mkdir -p soong-test/java/integration/provider

    cat > soong-test/java/integration/provider/Provider.java <<'EOF'
package soong.java.integration.provider;

public class Provider {
  public static final int PROVIDER_CONST = -1;

  private String className;

  public Provider() {
    this.className = "Provider";
  }

  public int previousProviderConst() {
    return PROVIDER_CONST - 1;
  }

  private String getProviderName() {
    return className;
  }
}
EOF
}

function modify_annotation_api {
  mkdir -p soong-test/java/integration/impllib

  cat > soong-test/java/integration/impllib/AnnotationUsage.java <<'EOF'
package soong.java.integration.impllib;
import soong.java.integration.annotation.GenerateWrapper;
public class AnnotationUsage {
  @GenerateWrapper(methodCount = 1)
  public String getClassName() {
    return "AnnotationUsage";
  }
}
EOF
}

function remove_base_dir {
    rm -rf soong-test
}

# shellcheck disable=SC2120
function partial_compile_setup {
  remove_base_dir
  create_base_files
  set_partial_compile_flags

  local target_to_remove="${1:-}"
  if [ -n "$target_to_remove" ]; then # Check if it's non-empty after default
      echo "Removing target: $target_to_remove"
      rm -rf "$target_to_remove"
  fi

  local all_files="${2:-}"
  local all_files_create=false
  if [[ "$all_files" == "true" ]]; then
      all_files_create=true
  fi
  if [[ "$all_files_create" == true ]]; then
      echo "Adding All Files:"
      create_example_impl3_file
  fi
}

function partial_compile_setup_with_ap {
  remove_base_dir
  create_base_files
  create_ap_files
  set_partial_compile_flags
}

# shellcheck disable=SC2120
function full_compile_setup {
  remove_base_dir
  create_base_files
  unset_partial_compile_flags

  local target_to_remove="${1:-}"
  if [ -n "$target_to_remove" ]; then # Check if it's non-empty after default
      echo "Removing target: $target_to_remove"
      rm -rf "$target_to_remove"
  fi

  local all_files="${2:-}"
  local all_files_create=false
  if [[ "$all_files" == "true" ]]; then
      all_files_create=true
  fi
  if [[ "$all_files_create" == true ]]; then
      echo "Adding All Files:"
      create_example_impl3_file
  fi
}

function set_partial_compile_flags {
    export SOONG_PARTIAL_COMPILE=true
    export SOONG_USE_PARTIAL_COMPILE=true
}

function unset_partial_compile_flags {
    export SOONG_PARTIAL_COMPILE=false
}
