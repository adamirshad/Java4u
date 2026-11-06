package dev.java4u.chunky.util;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A small JSON reader. Objects become {@code LinkedHashMap<String, Object>}, arrays
 * {@code List<Object>}, numbers {@code Long} or {@code Double}. Chunky can't use the
 * game's Gson because it runs before the game's libraries are loaded.
 */
public final class Json {
	private final String s;
	private int i;

	private Json(String s) {
		this.s = s;
	}

	public static Object parse(String text) {
		Json p = new Json(text);
		p.skipSpace();
		Object v = p.value();
		p.skipSpace();
		if (p.i != p.s.length()) throw p.error("unexpected trailing text");
		return v;
	}

	private IllegalArgumentException error(String msg) {
		int line = 1;
		for (int k = 0; k < Math.min(i, s.length()); k++) if (s.charAt(k) == '\n') line++;
		return new IllegalArgumentException("JSON error on line " + line + ": " + msg);
	}

	private void skipSpace() {
		while (i < s.length()) {
			char c = s.charAt(i);
			if (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '﻿') {
				i++;
			} else if (c == '/' && i + 1 < s.length() && s.charAt(i + 1) == '/') {
				// Some mods ship comments in their metadata; tolerate them like Fabric does.
				while (i < s.length() && s.charAt(i) != '\n') i++;
			} else {
				break;
			}
		}
	}

	private Object value() {
		if (i >= s.length()) throw error("unexpected end");
		char c = s.charAt(i);
		switch (c) {
		case '{':
			return object();
		case '[':
			return array();
		case '"':
			return string();
		case 't':
			return literal("true", Boolean.TRUE);
		case 'f':
			return literal("false", Boolean.FALSE);
		case 'n':
			return literal("null", null);
		default:
			if (c == '-' || (c >= '0' && c <= '9')) return number();
			throw error("unexpected character '" + c + "'");
		}
	}

	private Object literal(String word, Object v) {
		if (!s.startsWith(word, i)) throw error("expected " + word);
		i += word.length();
		return v;
	}

	private Map<String, Object> object() {
		Map<String, Object> m = new LinkedHashMap<>();
		i++;
		skipSpace();
		if (peek('}')) {
			i++;
			return m;
		}
		while (true) {
			skipSpace();
			if (!peek('"')) throw error("expected a key");
			String k = string();
			skipSpace();
			expect(':');
			skipSpace();
			m.put(k, value());
			skipSpace();
			if (peek(',')) {
				i++;
				skipSpace();
				if (peek('}')) { // tolerate a trailing comma
					i++;
					return m;
				}
				continue;
			}
			expect('}');
			return m;
		}
	}

	private List<Object> array() {
		List<Object> a = new ArrayList<>();
		i++;
		skipSpace();
		if (peek(']')) {
			i++;
			return a;
		}
		while (true) {
			skipSpace();
			a.add(value());
			skipSpace();
			if (peek(',')) {
				i++;
				skipSpace();
				if (peek(']')) {
					i++;
					return a;
				}
				continue;
			}
			expect(']');
			return a;
		}
	}

	private String string() {
		expect('"');
		StringBuilder sb = new StringBuilder();
		while (true) {
			if (i >= s.length()) throw error("unterminated string");
			char c = s.charAt(i++);
			if (c == '"') return sb.toString();
			if (c != '\\') {
				sb.append(c);
				continue;
			}
			if (i >= s.length()) throw error("bad escape");
			char e = s.charAt(i++);
			switch (e) {
			case 'n': sb.append('\n'); break;
			case 't': sb.append('\t'); break;
			case 'r': sb.append('\r'); break;
			case 'b': sb.append('\b'); break;
			case 'f': sb.append('\f'); break;
			case 'u':
				if (i + 4 > s.length()) throw error("bad unicode escape");
				sb.append((char) Integer.parseInt(s.substring(i, i + 4), 16));
				i += 4;
				break;
			default: sb.append(e);
			}
		}
	}

	private Object number() {
		int start = i;
		if (peek('-')) i++;
		boolean decimal = false;
		while (i < s.length()) {
			char c = s.charAt(i);
			if (c >= '0' && c <= '9') {
				i++;
			} else if (c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') {
				decimal = true;
				i++;
			} else {
				break;
			}
		}
		String n = s.substring(start, i);
		try {
			if (!decimal) return Long.parseLong(n);
			return Double.parseDouble(n);
		} catch (NumberFormatException e) {
			throw error("bad number " + n);
		}
	}

	private boolean peek(char c) {
		return i < s.length() && s.charAt(i) == c;
	}

	private void expect(char c) {
		if (!peek(c)) throw error("expected '" + c + "'");
		i++;
	}

	// ---------------------------------------------------------------- helpers for reading parsed JSON

	@SuppressWarnings("unchecked")
	public static Map<String, Object> obj(Object o) {
		return o instanceof Map ? (Map<String, Object>) o : null;
	}

	@SuppressWarnings("unchecked")
	public static List<Object> arr(Object o) {
		return o instanceof List ? (List<Object>) o : null;
	}

	public static String str(Object o) {
		return o instanceof String ? (String) o : null;
	}

	/** A string or array-of-strings field as a list. */
	public static List<String> strings(Object o) {
		List<String> out = new ArrayList<>();
		if (o instanceof String) {
			out.add((String) o);
		} else if (o instanceof List) {
			for (Object x : (List<?>) o) if (x instanceof String) out.add((String) x);
		}
		return out;
	}
}
