package net.fabricmc.api;

import java.lang.annotation.Documented;
import java.lang.annotation.ElementType;
import java.lang.annotation.Repeatable;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/** Marks an implemented interface that only exists on one side. */
@Retention(RetentionPolicy.CLASS)
@Target(ElementType.TYPE)
@Repeatable(EnvironmentInterfaces.class)
@Documented
public @interface EnvironmentInterface {
	EnvType value();

	Class<?> itf();
}
