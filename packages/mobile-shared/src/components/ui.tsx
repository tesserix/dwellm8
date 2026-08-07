import React from 'react';
import {
  View, Text, StyleSheet, Pressable, ScrollView, ViewStyle, TextStyle, StyleProp,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { color, font, radius, shadow, space } from '../theme/tokens';
import { ChevronDown, ChevronRight, UserIcon } from './icons';

/* ---------------------------------------------------------------- canvas */

export function Screen({ children, scroll = true }: { children: React.ReactNode; scroll?: boolean }) {
  const body = scroll ? (
    <ScrollView
      contentContainerStyle={{ paddingBottom: space(8) }}
      showsVerticalScrollIndicator={false}
    >
      {children}
    </ScrollView>
  ) : (
    <View style={{ flex: 1 }}>{children}</View>
  );
  return <View style={s.canvas}>{body}</View>;
}

export function AppHeader({
  title, onTitlePress, left, right, showCaret = true,
}: {
  title: string;
  onTitlePress?: () => void;
  left?: React.ReactNode;
  right?: React.ReactNode;
  showCaret?: boolean;
}) {
  return (
    <SafeAreaView edges={['top']} style={s.headerSafe}>
      <View style={s.header}>
        <View style={s.headerSide}>{left}</View>
        <Pressable style={s.headerTitleWrap} onPress={onTitlePress} disabled={!onTitlePress}>
          <Text style={s.headerTitle} numberOfLines={1}>{title}</Text>
          {showCaret && onTitlePress ? <ChevronDown size={18} w={2.4} /> : null}
        </Pressable>
        <View style={[s.headerSide, { alignItems: 'flex-end' }]}>{right}</View>
      </View>
    </SafeAreaView>
  );
}

export const AvatarButton = ({ onPress }: { onPress?: () => void }) => (
  <Pressable accessibilityRole="button" accessibilityLabel="Your profile" onPress={onPress} hitSlop={10}>
    <UserIcon size={30} w={1.7} />
  </Pressable>
);

/* ---------------------------------------------------------------- surfaces */

export function Card({
  children, style, padded = true,
}: { children: React.ReactNode; style?: StyleProp<ViewStyle>; padded?: boolean }) {
  return <View style={[s.card, padded && { padding: space(4) }, style]}>{children}</View>;
}

export function SectionTitle({ children, style }: { children: React.ReactNode; style?: StyleProp<TextStyle> }) {
  return <Text style={[s.sectionTitle, style]}>{children}</Text>;
}

export function CollapsibleHeader({
  title, open = true, onToggle,
}: { title: string; open?: boolean; onToggle?: () => void }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={title} style={s.collapseRow} onPress={onToggle}>
      <Text style={s.collapseTitle}>{title}</Text>
      <View style={{ transform: [{ rotate: open ? '0deg' : '-90deg' }] }}>
        <ChevronDown size={22} w={2.4} />
      </View>
    </Pressable>
  );
}

/* ---------------------------------------------------------------- controls */

export function Segmented({
  items, value, onChange,
}: { items: string[]; value: string; onChange: (v: string) => void }) {
  return (
    <View style={s.segWrap}>
      {items.map((it, i) => {
        const active = it === value;
        return (
          <React.Fragment key={it}>
            {i > 0 && !active && !(items[i - 1] === value) ? <View style={s.segDivider} /> : null}
            <Pressable
              accessibilityRole="tab"
              accessibilityLabel={it}
              accessibilityState={{ selected: active }}
              style={[s.segItem, active && s.segItemActive]}
              onPress={() => onChange(it)}
            >
              <Text style={[s.segText, active && s.segTextActive]}>{it}</Text>
            </Pressable>
          </React.Fragment>
        );
      })}
    </View>
  );
}

export function ChipRow({
  items, value, onChange,
}: { items: { label: string; icon?: React.ReactNode }[]; value: string; onChange: (v: string) => void }) {
  return (
    <View style={s.chipRow}>
      {items.map((it) => {
        const active = it.label === value;
        return (
          <Pressable
            key={it.label}
            accessibilityRole="button"
            accessibilityLabel={it.label}
            accessibilityState={{ selected: active }}
            onPress={() => onChange(it.label)}
            style={[s.chip, active && s.chipActive]}
          >
            {it.icon ? <View style={{ marginRight: 6 }}>{it.icon}</View> : null}
            <Text style={[s.chipText, active && s.chipTextActive]}>{it.label}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

/* ---------------------------------------------------------------- rows */

export function MoneyRow({
  label, value, tone = 'neutral', strong = false, last = false,
}: {
  label: string;
  value: string;
  tone?: 'neutral' | 'positive' | 'negative';
  strong?: boolean;
  last?: boolean;
}) {
  const tint =
    tone === 'positive' ? color.positive : tone === 'negative' ? color.negative : color.inkStrong;
  return (
    <View>
      <View style={s.moneyRow}>
        <Text style={[strong ? s.moneyLabelStrong : s.moneyLabel]}>{label}</Text>
        <Text style={[strong ? s.moneyValueStrong : s.moneyValue, { color: tint }]}>{value}</Text>
      </View>
      {!last ? <DottedRule /> : null}
    </View>
  );
}

export const DottedRule = () => <View style={s.dotted} />;

export function StatTile({
  value, label, tone = 'neutral',
}: { value: string; label: string; tone?: 'neutral' | 'positive' }) {
  return (
    <View style={[s.statTile, tone === 'positive' && { backgroundColor: color.positiveTint }]}>
      <Text style={[s.statValue, tone === 'positive' && { color: color.positive }]}>{value}</Text>
      <Text style={[s.statLabel, tone === 'positive' && { color: color.positive }]}>{label}</Text>
    </View>
  );
}

export function ActivityRow({
  icon, title, subtitle, meta, right, onPress,
}: {
  icon: React.ReactNode;
  title: string;
  subtitle?: React.ReactNode;
  meta?: string;
  right?: React.ReactNode;
  onPress?: () => void;
}) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={title} style={s.activity} onPress={onPress}>
      <View style={s.activityIcon}>{icon}</View>
      <View style={s.activityBar} />
      <View style={{ flex: 1 }}>
        <Text style={s.activityTitle}>{title}</Text>
        {subtitle}
        {meta ? <Text style={s.activityMeta}>{meta}</Text> : null}
      </View>
      {right}
    </Pressable>
  );
}

export function LinkRow({
  label, value, icon, onPress, last = false,
}: { label: string; value?: string; icon?: React.ReactNode; onPress?: () => void; last?: boolean }) {
  return (
    <View>
      <Pressable accessibilityRole="button" accessibilityLabel={label} style={s.linkRow} onPress={onPress}>
        <View style={{ flex: 1 }}>
          <Text style={s.linkLabel}>{label}</Text>
          {value ? <Text style={s.linkValue}>{value}</Text> : null}
        </View>
        {icon ? <View style={s.iconCircle}>{icon}</View> : <ChevronRight size={20} c={color.inkFaint} />}
      </Pressable>
      {!last ? <DottedRule /> : null}
    </View>
  );
}

/* ---------------------------------------------------------------- data viz */

export function ProgressBar({ pct, tint }: { pct: number; tint: string }) {
  return (
    <View style={s.track}>
      <View style={[s.fill, { width: `${Math.max(2, Math.min(100, pct))}%`, backgroundColor: tint }]} />
    </View>
  );
}

export function BarChart({
  data, max, highlightLast = true,
}: { data: { label: string; income: number; expense: number }[]; max: number; highlightLast?: boolean }) {
  const H = 150;
  return (
    <View>
      <View style={{ flexDirection: 'row' }}>
        <View style={{ width: 46, height: H, justifyContent: 'space-between' }}>
          {[4, 3, 2, 1, 0].map((k) => (
            <Text key={k} style={s.axis} numberOfLines={1}>
              {`₹${Math.round(((max / 100) * (k / 4)) / 1000)}K`}
            </Text>
          ))}
        </View>
        <View style={{ flex: 1 }}>
          <View style={{ height: H, flexDirection: 'row', alignItems: 'flex-end' }}>
            {[0, 1, 2, 3, 4].map((k) => (
              <View key={k} style={[s.grid, { bottom: (H / 4) * k }]} />
            ))}
            {data.map((d, i) => {
              const last = i === data.length - 1;
              return (
                <View key={d.label} style={s.barGroup}>
                  {highlightLast && last ? <View style={s.barBand} /> : null}
                  <View style={[s.bar, { height: Math.max(4, (d.income / max) * H), backgroundColor: '#5E8C3E' }]} />
                  <View style={[s.bar, { height: Math.max(4, (d.expense / max) * H), backgroundColor: '#B4453F', marginLeft: 5 }]} />
                </View>
              );
            })}
          </View>
          <View style={{ flexDirection: 'row', marginTop: 6 }}>
            {data.map((d, i) => (
              <Text
                key={d.label}
                style={[s.axisX, i === data.length - 1 && { color: '#D4772B', fontWeight: '700' }]}
              >
                {d.label}
              </Text>
            ))}
          </View>
        </View>
      </View>
    </View>
  );
}

/* ---------------------------------------------------------------- states */

export function EmptyState({ title, body, art }: { title: string; body: string; art?: React.ReactNode }) {
  return (
    <Card style={{ alignItems: 'center', paddingVertical: space(7) }}>
      {art}
      <Text style={s.emptyTitle}>{title}</Text>
      <Text style={s.emptyBody}>{body}</Text>
    </Card>
  );
}

/** A screen that could not load, with the way out of it (#343). */
export function ErrorState({ error, title = 'That did not load', onRetry }:
  { error: string; title?: string; onRetry?: () => void }) {
  return (
    <Card style={{ alignItems: 'center', paddingVertical: space(6) }}>
      <Text style={s.emptyTitle}>{title}</Text>
      <Text style={s.emptyBody}>{error}</Text>
      {onRetry ? (
        <Pressable accessibilityRole="button" onPress={onRetry} style={s.retry}>
          <Text style={s.retryText}>Try again</Text>
        </Pressable>
      ) : null}
    </Card>
  );
}

export function Banner({ children, onClose }: { children: React.ReactNode; onClose?: () => void }) {
  return <View style={s.banner}>{children}</View>;
}

export const Badge = ({ text, tone = 'blue' }: { text: string; tone?: 'blue' | 'green' }) => (
  <View style={[s.badge, tone === 'green' && { backgroundColor: '#EAF5E3' }]}>
    <Text style={[s.badgeText, tone === 'green' && { color: color.positive }]}>{text}</Text>
  </View>
);

/* ---------------------------------------------------------------- styles */

const s = StyleSheet.create({
  canvas: { flex: 1, backgroundColor: color.bgTop },

  headerSafe: { backgroundColor: color.headerBar },
  header: {
    flexDirection: 'row', alignItems: 'center',
    paddingHorizontal: space(4), paddingVertical: space(3),
    backgroundColor: color.headerBar,
  },
  headerSide: { width: 44, justifyContent: 'center' },
  headerTitleWrap: { flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 4 },
  headerTitle: { ...font.h3, color: color.accent, maxWidth: '82%' },

  card: {
    backgroundColor: color.card, borderRadius: radius.lg,
    marginHorizontal: space(4), marginBottom: space(3), ...shadow.card,
  },
  sectionTitle: {
    ...font.h2, color: color.inkStrong,
    marginHorizontal: space(4), marginTop: space(5), marginBottom: space(3),
  },
  collapseRow: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between',
    marginHorizontal: space(4), marginTop: space(4), marginBottom: space(2),
  },
  collapseTitle: { ...font.h2, color: color.inkStrong },

  segWrap: {
    flexDirection: 'row', alignItems: 'center',
    backgroundColor: color.card, borderRadius: radius.pill,
    borderWidth: 1, borderColor: color.line,
    marginHorizontal: space(4), padding: 4, marginBottom: space(1),
  },
  segItem: { flex: 1, paddingVertical: 9, borderRadius: radius.pill, alignItems: 'center' },
  segItemActive: { backgroundColor: color.segmentFrom },
  segDivider: { width: 1, height: 18, backgroundColor: color.line },
  segText: { ...font.label, color: color.accent },
  segTextActive: { color: '#FFF' },

  chipRow: { flexDirection: 'row', gap: 10, paddingHorizontal: space(4), marginVertical: space(3) },
  chip: {
    flexDirection: 'row', alignItems: 'center',
    paddingHorizontal: 16, paddingVertical: 9, borderRadius: radius.pill,
    backgroundColor: color.chipIdle, borderWidth: 1, borderColor: color.line,
  },
  chipActive: { backgroundColor: color.accent, borderColor: color.accent },
  chipText: { ...font.label, color: color.accent },
  chipTextActive: { color: '#FFF' },

  moneyRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingVertical: 11 },
  moneyLabel: { ...font.body, color: color.ink },
  moneyValue: { ...font.body, fontWeight: '600' },
  moneyLabelStrong: { ...font.h3, color: color.inkStrong },
  moneyValueStrong: { ...font.h3, fontWeight: '800' },
  dotted: { borderBottomWidth: 1, borderStyle: 'dotted', borderColor: color.lineDotted },

  statTile: {
    flex: 1, backgroundColor: color.cardMuted, borderRadius: radius.md,
    paddingVertical: space(3), alignItems: 'center',
  },
  statValue: { ...font.title, color: color.inkStrong },
  statLabel: { ...font.small, color: color.inkSoft, marginTop: 2 },

  activity: {
    flexDirection: 'row', alignItems: 'center', gap: 12,
    backgroundColor: '#FCFDFE', borderRadius: radius.md,
    padding: space(3), marginHorizontal: space(4), marginBottom: space(2),
    borderWidth: 1, borderColor: '#EFF3F7',
  },
  activityIcon: { width: 30, alignItems: 'center' },
  activityBar: { width: 1.5, alignSelf: 'stretch', backgroundColor: color.line, marginRight: 4 },
  activityTitle: { ...font.title, color: color.accent },
  activityMeta: { ...font.small, color: color.inkSoft, marginTop: 3 },

  linkRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 13, gap: 10 },
  linkLabel: { ...font.body, color: color.inkStrong, fontWeight: '600' },
  linkValue: { ...font.small, color: color.inkSoft, marginTop: 2 },
  iconCircle: {
    width: 40, height: 40, borderRadius: 20, borderWidth: 1.6, borderColor: color.accent,
    alignItems: 'center', justifyContent: 'center',
  },

  track: { height: 9, borderRadius: 5, backgroundColor: '#DDE3E9', overflow: 'hidden', marginTop: 7 },
  fill: { height: '100%', borderRadius: 5 },

  axis: { ...font.small, color: color.inkSoft, textAlign: 'right', paddingRight: 6 },
  axisX: { flex: 1, ...font.small, color: color.inkSoft, textAlign: 'center' },
  grid: { position: 'absolute', left: 0, right: 0, height: 1, backgroundColor: '#EDF1F5' },
  barGroup: { flex: 1, flexDirection: 'row', alignItems: 'flex-end', justifyContent: 'center' },
  barBand: { position: 'absolute', top: -150, bottom: 0, left: 2, right: 2, backgroundColor: '#FBECE6', borderRadius: 4 },
  bar: { width: 9, borderRadius: 5 },

  emptyTitle: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  emptyBody: { ...font.body, color: color.inkSoft, textAlign: 'center', marginTop: 6, paddingHorizontal: space(4) },

  retry: {
    marginTop: space(4), paddingVertical: space(2), paddingHorizontal: space(5),
    borderRadius: radius.md, borderWidth: 1, borderColor: color.accent,
  },
  retryText: { ...font.body, color: color.accent, fontWeight: '600' },

  banner: {
    flexDirection: 'row', alignItems: 'center', gap: 10,
    backgroundColor: color.warnBg, borderRadius: radius.md,
    padding: space(3), marginHorizontal: space(4), marginVertical: space(2),
  },

  badge: { backgroundColor: '#E7EDF8', borderRadius: radius.pill, paddingHorizontal: 12, paddingVertical: 5, alignSelf: 'flex-start' },
  badgeText: { ...font.tiny, color: color.segmentTo },
});

export const ui = s;
