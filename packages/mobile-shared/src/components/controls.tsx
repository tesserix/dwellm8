import React from 'react';
import {
  View, Text, StyleSheet, Pressable, TextInput, Switch, StyleProp, ViewStyle,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { color, font, radius, shadow, space } from '../theme/tokens';
import { CheckIcon, ChevronLeft, ChevronRight, SearchIcon } from './icons';

/**
 * The working parts — buttons, pills, rows, inputs and timelines.
 *
 * Ops, Pro and Admin are worklist apps: the same row, pill and button appear
 * hundreds of times a day, so they live here once and are never restyled per
 * app. Anything an app needs and cannot find here belongs here, not there.
 */

/* ---------------------------------------------------------------- actions */

type ButtonTone = 'primary' | 'secondary' | 'danger' | 'ghost';

export function Button({
  label, onPress, tone = 'primary', icon, disabled = false, style, small = false,
}: {
  label: string;
  onPress?: () => void;
  tone?: ButtonTone;
  icon?: React.ReactNode;
  disabled?: boolean;
  style?: StyleProp<ViewStyle>;
  small?: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled }}
      onPress={disabled ? undefined : onPress}
      style={({ pressed }) => [
        s.btn,
        small && s.btnSmall,
        tone === 'primary' && s.btnPrimary,
        tone === 'secondary' && s.btnSecondary,
        tone === 'danger' && s.btnDanger,
        tone === 'ghost' && s.btnGhost,
        disabled && { opacity: 0.45 },
        pressed && !disabled && { opacity: 0.85 },
        style,
      ]}
    >
      {icon ? <View style={{ marginRight: 8 }}>{icon}</View> : null}
      <Text
        style={[
          s.btnText,
          small && { fontSize: 14 },
          (tone === 'secondary' || tone === 'ghost') && { color: color.accent },
        ]}
      >
        {label}
      </Text>
    </Pressable>
  );
}

/** The bar that carries the one decision on a detail screen. */
export function ActionBar({ children }: { children: React.ReactNode }) {
  return (
    <SafeAreaView edges={['bottom']} style={s.actionBar}>
      <View style={s.actionBarInner}>{children}</View>
    </SafeAreaView>
  );
}

export function BackHeader({
  title, subtitle, onBack, right,
}: { title: string; subtitle?: string; onBack?: () => void; right?: React.ReactNode }) {
  return (
    <SafeAreaView edges={['top']} style={s.backSafe}>
      <View style={s.backRow}>
        <Pressable accessibilityRole="button" accessibilityLabel="Back" onPress={onBack} hitSlop={12} style={s.backBtn}>
          <ChevronLeft size={24} w={2.2} />
        </Pressable>
        <View style={{ flex: 1 }}>
          <Text style={s.backTitle} numberOfLines={1}>{title}</Text>
          {subtitle ? <Text style={s.backSub} numberOfLines={1}>{subtitle}</Text> : null}
        </View>
        {right}
      </View>
    </SafeAreaView>
  );
}

/* ---------------------------------------------------------------- status */

export type Tone = 'neutral' | 'green' | 'amber' | 'red' | 'blue' | 'violet';

const toneInk: Record<Tone, { bg: string; ink: string }> = {
  neutral: { bg: '#EEF2F7', ink: '#5C6B7A' },
  green: { bg: '#E8F4E0', ink: '#4E8A2C' },
  amber: { bg: '#FBEEDC', ink: '#B0731C' },
  red: { bg: '#FBE6E4', ink: '#C0433D' },
  blue: { bg: '#E4EEF9', ink: '#3667A6' },
  violet: { bg: '#EDEAFA', ink: '#5B4CC4' },
};

export function StatusPill({ text, tone = 'neutral', dot = false }: { text: string; tone?: Tone; dot?: boolean }) {
  const t = toneInk[tone];
  return (
    <View style={[s.pill, { backgroundColor: t.bg }]}>
      {dot ? <View style={[s.pillDot, { backgroundColor: t.ink }]} /> : null}
      <Text style={[s.pillText, { color: t.ink }]}>{text}</Text>
    </View>
  );
}

export const toneColor = (t: Tone) => toneInk[t].ink;

/* ---------------------------------------------------------------- rows */

export function ListRow({
  title, subtitle, meta, right, left, onPress, last = false, tone,
}: {
  title: string;
  subtitle?: string;
  meta?: string;
  right?: React.ReactNode;
  left?: React.ReactNode;
  onPress?: () => void;
  last?: boolean;
  tone?: Tone;
}) {
  return (
    <View>
      <Pressable accessibilityRole="button" accessibilityLabel={title} style={s.row} onPress={onPress}>
        {tone ? <View style={[s.rowStripe, { backgroundColor: toneInk[tone].ink }]} /> : null}
        {left ? <View style={{ marginRight: 12 }}>{left}</View> : null}
        <View style={{ flex: 1 }}>
          <Text style={s.rowTitle} numberOfLines={1}>{title}</Text>
          {subtitle ? <Text style={s.rowSub} numberOfLines={2}>{subtitle}</Text> : null}
          {meta ? <Text style={s.rowMeta}>{meta}</Text> : null}
        </View>
        {right ?? <ChevronRight size={19} c={color.inkFaint} />}
      </Pressable>
      {!last ? <View style={s.hair} /> : null}
    </View>
  );
}

export function KeyValue({ k, v, tone, last = false }: { k: string; v: string; tone?: Tone; last?: boolean }) {
  return (
    <View>
      <View style={s.kv}>
        <Text style={s.kvKey}>{k}</Text>
        <Text style={[s.kvVal, tone ? { color: toneInk[tone].ink } : null]}>{v}</Text>
      </View>
      {!last ? <View style={s.hair} /> : null}
    </View>
  );
}

export function Avatar({ initials, size = 38, tone = 'blue' }: { initials: string; size?: number; tone?: Tone }) {
  const t = toneInk[tone];
  return (
    <View style={[s.avatar, { width: size, height: size, borderRadius: size / 2, backgroundColor: t.bg }]}>
      <Text style={[s.avatarText, { color: t.ink, fontSize: size * 0.36 }]}>{initials}</Text>
    </View>
  );
}

/* ---------------------------------------------------------------- inputs */

export function SearchBar({
  value, onChange, placeholder = 'Search',
}: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <View style={s.search}>
      <SearchIcon size={19} c={color.inkSoft} />
      <TextInput
        value={value}
        onChangeText={onChange}
        placeholder={placeholder}
        placeholderTextColor={color.inkFaint}
        style={s.searchInput}
      />
    </View>
  );
}

export function Field({
  label, value, onChange, placeholder, multiline = false, keyboardType,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  multiline?: boolean;
  keyboardType?: 'default' | 'numeric' | 'phone-pad';
}) {
  return (
    <View style={{ marginBottom: space(4) }}>
      <Text style={s.fieldLabel}>{label}</Text>
      <TextInput
        value={value}
        onChangeText={onChange}
        placeholder={placeholder}
        placeholderTextColor={color.inkFaint}
        multiline={multiline}
        keyboardType={keyboardType}
        style={[s.field, multiline && { height: 104, textAlignVertical: 'top', paddingTop: 12 }]}
      />
    </View>
  );
}

export function SwitchRow({
  label, hint, value, onChange, last = false,
}: { label: string; hint?: string; value: boolean; onChange: (v: boolean) => void; last?: boolean }) {
  return (
    <View>
      <View style={s.switchRow}>
        <View style={{ flex: 1, paddingRight: 12 }}>
          <Text style={s.rowTitle}>{label}</Text>
          {hint ? <Text style={s.rowSub}>{hint}</Text> : null}
        </View>
        <Switch
          value={value}
          onValueChange={onChange}
          trackColor={{ true: color.accent, false: '#CBD5E0' }}
          thumbColor="#FFFFFF"
        />
      </View>
      {!last ? <View style={s.hair} /> : null}
    </View>
  );
}

export function ChoiceRow({
  label, hint, selected, onPress, last = false,
}: { label: string; hint?: string; selected: boolean; onPress: () => void; last?: boolean }) {
  return (
    <View>
      <Pressable accessibilityRole="radio" accessibilityLabel={label} accessibilityState={{ selected }} style={s.switchRow} onPress={onPress}>
        <View style={{ flex: 1, paddingRight: 12 }}>
          <Text style={s.rowTitle}>{label}</Text>
          {hint ? <Text style={s.rowSub}>{hint}</Text> : null}
        </View>
        <View style={[s.radio, selected && { borderColor: color.accent, backgroundColor: color.accent }]}>
          {selected ? <CheckIcon size={14} c="#FFF" w={2.6} /> : null}
        </View>
      </Pressable>
      {!last ? <View style={s.hair} /> : null}
    </View>
  );
}

/** Three-state capture used by every inspection and condition checklist. */
export function TriState({
  label, value, onChange,
}: { label: string; value: 'ok' | 'note' | 'issue' | null; onChange: (v: 'ok' | 'note' | 'issue') => void }) {
  const opts: { k: 'ok' | 'note' | 'issue'; t: string; tone: Tone }[] = [
    { k: 'ok', t: 'OK', tone: 'green' },
    { k: 'note', t: 'Note', tone: 'amber' },
    { k: 'issue', t: 'Issue', tone: 'red' },
  ];
  return (
    <View style={s.tri}>
      <Text style={[s.rowTitle, { flex: 1 }]} numberOfLines={1}>{label}</Text>
      <View style={{ flexDirection: 'row', gap: 6 }}>
        {opts.map((o) => {
          const on = value === o.k;
          return (
            <Pressable
              key={o.k}
              accessibilityRole="button"
              accessibilityLabel={`${label}: ${o.t}`}
              onPress={() => onChange(o.k)}
              style={[
                s.triBtn,
                on && { backgroundColor: toneInk[o.tone].bg, borderColor: toneInk[o.tone].ink },
              ]}
            >
              <Text style={[s.triText, on && { color: toneInk[o.tone].ink }]}>{o.t}</Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

/* ---------------------------------------------------------------- display */

export function Metric({
  value, label, tone = 'neutral', onPress,
}: { value: string; label: string; tone?: Tone; onPress?: () => void }) {
  const t = toneInk[tone];
  return (
    <Pressable
      accessibilityRole={onPress ? 'button' : 'text'}
      accessibilityLabel={`${value} ${label}`}
      style={[s.metric, { backgroundColor: t.bg }]}
      onPress={onPress}
    >
      <Text style={[s.metricValue, { color: t.ink }]} numberOfLines={1}>{value}</Text>
      <Text style={[s.metricLabel, { color: t.ink }]} numberOfLines={2}>{label}</Text>
    </Pressable>
  );
}

export function Timeline({ items }: { items: { at: string; what: string; done?: boolean }[] }) {
  return (
    <View style={{ paddingTop: space(1) }}>
      {items.map((it, i) => {
        const last = i === items.length - 1;
        return (
          <View key={`${it.at}-${i}`} style={{ flexDirection: 'row' }}>
            <View style={{ width: 22, alignItems: 'center' }}>
              <View style={[s.tlDot, it.done === false && { backgroundColor: '#FFF', borderColor: color.lineDotted }]} />
              {!last ? <View style={s.tlLine} /> : null}
            </View>
            <View style={{ flex: 1, paddingBottom: last ? 0 : space(4), paddingLeft: 10 }}>
              <Text style={s.tlWhat}>{it.what}</Text>
              <Text style={s.tlAt}>{it.at}</Text>
            </View>
          </View>
        );
      })}
    </View>
  );
}

/** Placeholder tiles standing in for camera evidence until capture is wired. */
export function PhotoStrip({ count, onAdd }: { count: number; onAdd?: () => void }) {
  return (
    <View style={s.photoRow}>
      {Array.from({ length: count }).map((_, i) => (
        <View key={i} style={s.photo}>
          <View style={s.photoSky} />
          <View style={s.photoGround} />
        </View>
      ))}
      {onAdd ? (
        <Pressable accessibilityRole="button" accessibilityLabel="Add a photo" style={[s.photo, s.photoAdd]} onPress={onAdd}>
          <Text style={s.photoAddText}>+</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

export function Toast({ text }: { text: string }) {
  return (
    <View style={s.toast}>
      <CheckIcon size={17} c="#FFF" w={2.4} />
      <Text style={s.toastText}>{text}</Text>
    </View>
  );
}

/** Offline/queued indicator — the apps must never claim a write succeeded. */
export function SyncBadge({ queued }: { queued: number }) {
  if (!queued) return null;
  return (
    <View style={s.sync}>
      <Text style={s.syncText}>{queued} queued · syncing when back online</Text>
    </View>
  );
}

const s = StyleSheet.create({
  btn: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'center',
    paddingVertical: space(4), paddingHorizontal: space(5), borderRadius: radius.pill,
  },
  btnSmall: { paddingVertical: space(2.5), paddingHorizontal: space(4) },
  btnPrimary: { backgroundColor: color.accent },
  btnSecondary: { backgroundColor: '#FFF', borderWidth: 1.5, borderColor: color.accent },
  btnDanger: { backgroundColor: color.negative },
  btnGhost: { backgroundColor: 'transparent' },
  btnText: { ...font.title, color: '#FFF' },

  actionBar: { backgroundColor: '#FFF', borderTopWidth: 1, borderTopColor: color.line, ...shadow.bar },
  actionBarInner: { flexDirection: 'row', gap: 10, padding: space(4) },

  backSafe: { backgroundColor: color.headerBar },
  backRow: {
    flexDirection: 'row', alignItems: 'center', gap: 10,
    paddingHorizontal: space(3), paddingVertical: space(3), backgroundColor: color.headerBar,
  },
  backBtn: { width: 32, height: 32, alignItems: 'center', justifyContent: 'center' },
  backTitle: { ...font.h3, color: color.inkStrong },
  backSub: { ...font.small, color: color.inkSoft, marginTop: 1 },

  pill: {
    flexDirection: 'row', alignItems: 'center', gap: 6,
    paddingHorizontal: 10, paddingVertical: 5, borderRadius: radius.pill, alignSelf: 'flex-start',
  },
  pillDot: { width: 6, height: 6, borderRadius: 3 },
  pillText: { ...font.tiny, fontSize: 11 },

  row: { flexDirection: 'row', alignItems: 'center', paddingVertical: 13, gap: 10 },
  rowStripe: { width: 3, alignSelf: 'stretch', borderRadius: 2, marginRight: 6 },
  rowTitle: { ...font.body, color: color.inkStrong, fontWeight: '700' },
  rowSub: { ...font.small, color: color.inkSoft, marginTop: 3, lineHeight: 18 },
  rowMeta: { ...font.small, color: color.inkFaint, marginTop: 3 },
  hair: { height: 1, backgroundColor: '#EEF2F6' },

  kv: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingVertical: 11, gap: 16 },
  kvKey: { ...font.body, color: color.inkSoft, flexShrink: 0 },
  kvVal: { ...font.body, color: color.inkStrong, fontWeight: '700', flex: 1, textAlign: 'right' },

  avatar: { alignItems: 'center', justifyContent: 'center' },
  avatarText: { fontWeight: '800' },

  search: {
    flexDirection: 'row', alignItems: 'center', gap: 9,
    backgroundColor: '#FFF', borderRadius: radius.pill, borderWidth: 1, borderColor: color.line,
    paddingHorizontal: space(4), marginHorizontal: space(4), marginBottom: space(3), height: 44,
  },
  searchInput: { flex: 1, ...font.body, color: color.inkStrong },

  fieldLabel: { ...font.label, color: color.inkSoft, marginBottom: 7 },
  field: {
    backgroundColor: '#FFF', borderRadius: radius.md, borderWidth: 1, borderColor: color.line,
    paddingHorizontal: space(4), height: 48, ...font.body, color: color.inkStrong,
  },

  switchRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 13 },
  radio: {
    width: 24, height: 24, borderRadius: 12, borderWidth: 1.8, borderColor: color.lineDotted,
    alignItems: 'center', justifyContent: 'center',
  },

  tri: { flexDirection: 'row', alignItems: 'center', paddingVertical: 11, gap: 10 },
  triBtn: {
    paddingHorizontal: 11, paddingVertical: 6, borderRadius: radius.pill,
    borderWidth: 1, borderColor: color.line, backgroundColor: '#FFF',
  },
  triText: { ...font.small, color: color.inkSoft, fontWeight: '700' },

  metric: { flex: 1, borderRadius: radius.md, paddingVertical: space(3), paddingHorizontal: space(3) },
  metricValue: { ...font.h2, fontSize: 20 },
  metricLabel: { ...font.small, marginTop: 2, opacity: 0.85 },

  tlDot: { width: 11, height: 11, borderRadius: 6, backgroundColor: color.accent, marginTop: 4, borderWidth: 1.5, borderColor: color.accent },
  tlLine: { flex: 1, width: 1.5, backgroundColor: color.lineDotted, marginTop: 2 },
  tlWhat: { ...font.body, color: color.inkStrong },
  tlAt: { ...font.small, color: color.inkSoft, marginTop: 2 },

  photoRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: space(3) },
  photo: { width: 68, height: 68, borderRadius: radius.md, overflow: 'hidden', backgroundColor: '#DDE7F2' },
  photoSky: { height: 40, backgroundColor: '#CFE0F0' },
  photoGround: { flex: 1, backgroundColor: '#BFD3C4' },
  photoAdd: {
    backgroundColor: '#FFF', borderWidth: 1.5, borderColor: color.line, borderStyle: 'dashed',
    alignItems: 'center', justifyContent: 'center',
  },
  photoAddText: { fontSize: 26, color: color.accent, fontWeight: '300', marginTop: -4 },

  toast: {
    flexDirection: 'row', alignItems: 'center', gap: 8,
    backgroundColor: '#3E7C25', borderRadius: radius.pill,
    paddingHorizontal: space(4), paddingVertical: space(3),
    marginHorizontal: space(4), marginBottom: space(2),
  },
  toastText: { ...font.label, color: '#FFF' },

  sync: {
    backgroundColor: '#FFF6E5', borderRadius: radius.sm,
    paddingHorizontal: space(3), paddingVertical: 6, marginHorizontal: space(4), marginBottom: space(2),
    alignSelf: 'flex-start',
  },
  syncText: { ...font.small, color: '#96701C', fontWeight: '600' },
});

export const controls = s;
