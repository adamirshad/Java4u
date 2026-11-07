package dev.java4u.chunky.util;

import static dev.java4u.chunky.test.Check.*;

import java.util.List;
import java.util.Map;

public final class JsonTest {
	private JsonTest() {
	}

	public static void testParsesValues() {
		Map<String, Object> o = Json.obj(Json.parse(
				"{\"s\": \"a\\\"b\\u00e9\", \"n\": 12, \"d\": 1.5, \"neg\": -3, \"t\": true, \"f\": false, \"z\": null, \"a\": [1, \"x\"]}"));
		eq("a\"bé", o.get("s"), "string with escapes");
		eq(12L, o.get("n"), "integer");
		eq(1.5, o.get("d"), "decimal");
		eq(-3L, o.get("neg"), "negative");
		eq(Boolean.TRUE, o.get("t"), "true");
		eq(Boolean.FALSE, o.get("f"), "false");
		isTrue(o.containsKey("z") && o.get("z") == null, "null kept as a key");
		eq(List.of(1L, "x"), o.get("a"), "array");
	}

	public static void testKeepsKeyOrder() {
		Map<String, Object> o = Json.obj(Json.parse("{\"b\": 1, \"a\": 2, \"c\": 3}"));
		eq(List.of("b", "a", "c"), List.copyOf(o.keySet()), "key order");
	}

	public static void testToleratesCommentsAndBom() {
		Object v = Json.parse("﻿// a mod's comment\n{\n  // another\n  \"id\": \"x\"\n}\n");
		eq("x", Json.obj(v).get("id"), "value after comments");
	}

	public static void testErrorsNameTheLine() {
		IllegalArgumentException e = fails(IllegalArgumentException.class,
				() -> Json.parse("{\n\"a\": 1,\n\"b\": }"), "bad value");
		contains(e.getMessage(), "line 3", "error line");
		fails(IllegalArgumentException.class, () -> Json.parse("{} trailing"), "trailing text");
		fails(IllegalArgumentException.class, () -> Json.parse("[1, 2"), "unclosed array");
		fails(IllegalArgumentException.class, () -> Json.parse(""), "empty input");
	}

	public static void testHelpers() {
		eq(null, Json.obj("text"), "obj of a string");
		eq(null, Json.arr(Map.of()), "arr of an object");
		eq(null, Json.str(5L), "str of a number");
		eq(List.of("a", "b"), Json.strings(Json.parse("[\"a\", \"b\"]")), "strings of an array");
		eq(List.of("a"), Json.strings("a"), "strings of one string");
		eq(List.of(), Json.strings(null), "strings of nothing");
	}
}
