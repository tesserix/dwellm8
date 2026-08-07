import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Segmented, SectionTitle, ListRow, StatusPill, Metric,
  AlertIcon, CalendarIcon, RupeeIcon,
  color, count, font, inr, inrShort, space, type OpsReminder,
} from '@dwellm8/mobile-shared';
import { useReminders } from '../src/data/reminders';

/**
 * What is about to happen, building by building (#337).
 *
 * Collections answers for money already lost. This answers for the week ahead:
 * the rent falling due on each tenanted flat, what has already slipped, and the
 * terms running out while there is still time to serve notice.
 */

const windows: Record<string, number> = { 'Next 7 days': 7, 'Next 30 days': 30, 'Next 90 days': 90 };

const face: Record<OpsReminder['kind'], { icon: React.ReactNode; tone: 'red' | 'amber' | 'blue' }> = {
  rent_overdue: { icon: <AlertIcon size={20} c={color.negative} />, tone: 'red' },
  rent_due: { icon: <RupeeIcon size={20} c={color.accent} />, tone: 'blue' },
  tenancy_ending: { icon: <CalendarIcon size={20} c="#B0731C" />, tone: 'amber' },
};

function when(r: OpsReminder): string {
  if (r.kind === 'rent_overdue') return 'overdue now';
  if (r.days_away <= 0) return 'today';
  if (r.days_away === 1) return 'tomorrow';
  return `in ${count(r.days_away, 'day')} · ${r.on}`;
}

function headline(r: OpsReminder): string {
  const where = `${r.unit}, ${r.property}`;
  if (r.kind === 'tenancy_ending') return `${where} — term ends`;
  return `${where} — ${inr(r.amount_minor ?? 0, { noPaise: true })}`;
}

export default function Reminders() {
  const router = useRouter();
  const [tab, setTab] = useState('Next 30 days');
  const view = useReminders(windows[tab]);

  return (
    <>
      <BackHeader
        title="What is coming"
        subtitle="Rent falling due, arrears, and terms running out"
        onBack={() => router.back()}
      />
      <Screen>
        <View style={{ marginTop: space(3) }}>
          <Segmented items={Object.keys(windows)} value={tab} onChange={setTab} />
        </View>

        <View style={s.metrics}>
          <Metric
            value={view.loading ? '…' : inrShort(view.duePaise)}
            label="rent falling due"
            tone="blue"
          />
          <Metric
            value={view.loading ? '…' : inrShort(view.overduePaise)}
            label="already overdue"
            tone={view.overduePaise ? 'red' : 'green'}
            onPress={() => router.push('/(tabs)/collect')}
          />
          <Metric
            value={view.loading ? '…' : String(view.endingCount)}
            label="terms running out"
            tone={view.endingCount ? 'amber' : 'neutral'}
          />
        </View>

        {view.loading ? (
          <Card><View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View></Card>
        ) : null}
        {view.error ? <Card><Text style={s.empty}>{view.error}</Text></Card> : null}

        {view.properties.map((p) => (
          <View key={p.id}>
            <SectionTitle>{p.locality ? `${p.name} · ${p.locality}` : p.name}</SectionTitle>
            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              {p.reminders.map((r, i) => (
                <ListRow
                  key={`${r.kind}:${r.lease_id}`}
                  left={face[r.kind].icon}
                  title={headline(r)}
                  subtitle={when(r)}
                  meta={r.kind === 'tenancy_ending' && r.inside_notice_window
                    ? 'inside the notice period — a renewal decision is due now'
                    : undefined}
                  right={<StatusPill
                    text={r.kind === 'rent_overdue' ? 'Overdue'
                      : r.kind === 'tenancy_ending' ? 'Ending' : 'Due'}
                    tone={face[r.kind].tone}
                  />}
                  onPress={() => router.push(`/arrear?id=${r.lease_id}`)}
                  last={i === p.reminders.length - 1}
                />
              ))}
            </Card>
          </View>
        ))}

        {!view.loading && !view.error && !view.properties.length ? (
          <Card>
            <Text style={s.empty}>
              Nothing falls due in this window. Widen it, or the book is genuinely quiet.
            </Text>
          </Card>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(3), marginBottom: space(1) },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(5), textAlign: 'center' },
});
