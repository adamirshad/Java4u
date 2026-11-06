package dev.java4u.chunky.mod;

import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import net.fabricmc.loader.api.metadata.CustomValue;
import net.fabricmc.loader.api.metadata.CustomValue.CvType;

/** Wraps parsed JSON as {@link CustomValue}s for the "custom" metadata section. */
final class CustomValues {
	private CustomValues() {
	}

	static CustomValue of(Object json) {
		if (json instanceof Map) {
			Map<String, CustomValue> m = new LinkedHashMap<>();
			for (Map.Entry<?, ?> e : ((Map<?, ?>) json).entrySet()) m.put((String) e.getKey(), of(e.getValue()));
			return new Obj(m);
		}
		if (json instanceof List) {
			List<CustomValue> l = new ArrayList<>();
			for (Object o : (List<?>) json) l.add(of(o));
			return new Arr(l);
		}
		if (json instanceof String) return new Leaf(CvType.STRING, json);
		if (json instanceof Number) return new Leaf(CvType.NUMBER, json);
		if (json instanceof Boolean) return new Leaf(CvType.BOOLEAN, json);
		return new Leaf(CvType.NULL, null);
	}

	private abstract static class Base implements CustomValue {
		private final CvType type;

		Base(CvType type) {
			this.type = type;
		}

		@Override
		public CvType getType() {
			return type;
		}

		private ClassCastException wrong(String want) {
			return new ClassCastException("Custom value is " + type + ", not " + want);
		}

		@Override
		public CvObject getAsObject() {
			if (this instanceof CvObject) return (CvObject) this;
			throw wrong("OBJECT");
		}

		@Override
		public CvArray getAsArray() {
			if (this instanceof CvArray) return (CvArray) this;
			throw wrong("ARRAY");
		}

		@Override
		public String getAsString() {
			throw wrong("STRING");
		}

		@Override
		public Number getAsNumber() {
			throw wrong("NUMBER");
		}

		@Override
		public boolean getAsBoolean() {
			throw wrong("BOOLEAN");
		}
	}

	private static final class Leaf extends Base {
		private final Object value;

		Leaf(CvType type, Object value) {
			super(type);
			this.value = value;
		}

		@Override
		public String getAsString() {
			return getType() == CvType.STRING ? (String) value : super.getAsString();
		}

		@Override
		public Number getAsNumber() {
			return getType() == CvType.NUMBER ? (Number) value : super.getAsNumber();
		}

		@Override
		public boolean getAsBoolean() {
			return getType() == CvType.BOOLEAN ? (Boolean) value : super.getAsBoolean();
		}
	}

	private static final class Obj extends Base implements CustomValue.CvObject {
		private final Map<String, CustomValue> map;

		Obj(Map<String, CustomValue> map) {
			super(CvType.OBJECT);
			this.map = map;
		}

		@Override
		public int size() {
			return map.size();
		}

		@Override
		public boolean containsKey(String key) {
			return map.containsKey(key);
		}

		@Override
		public CustomValue get(String key) {
			return map.get(key);
		}

		@Override
		public Iterator<Map.Entry<String, CustomValue>> iterator() {
			return map.entrySet().iterator();
		}
	}

	private static final class Arr extends Base implements CustomValue.CvArray {
		private final List<CustomValue> list;

		Arr(List<CustomValue> list) {
			super(CvType.ARRAY);
			this.list = list;
		}

		@Override
		public int size() {
			return list.size();
		}

		@Override
		public CustomValue get(int index) {
			return list.get(index);
		}

		@Override
		public Iterator<CustomValue> iterator() {
			return list.iterator();
		}
	}
}
