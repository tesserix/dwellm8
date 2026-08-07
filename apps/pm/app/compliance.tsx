import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Metric, Button, KeyValue,
  Toast, ChipRow, ShieldIcon, AlertIcon,
  color, count, font, inr, inrShort, space, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { compliance } from '../src/data/mock';

/**
 * The compliance register.
 *
 * Everything with an expiry date, who owns it, and what it costs to renew.
 * Sorted by what expires first, because that is the only order that prevents
 * the thing this screen exists to prevent.
 */

const stateTone: Record<string, Tone> = { Current: 'green', 'Due soon': 'amber', Expired: 'red' };

export default function Compliance() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const [filter, setFilter] = useState('All');
  const [toast, setToast] = useState<string | null>(null);
  const [open, setOpen] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const list = compliance
    .filter((c) => filter === 'All' || (filter === 'Needs action' ? c.state !== 'Current' : c.owner === filter))
    .slice()
    .sort((a, b) => a.daysLeft - b.daysLeft);

  const expired = compliance.filter((c) => c.state === 'Expired').length;
  const soon = compliance.filter((c) => c.state === 'Due soon').length;
  const annual = compliance.reduce((a, c) => a + c.costPaise, 0);

  return (
    <>
      <BackHeader title="Compliance register" subtitle="Every certificate, and who owns it" onBack={goBack} />
      <Screen>
        <DemoNote issue={301} />
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={String(expired)} label="expired" tone="red" />
          <Metric value={String(soon)} label="due within 60 days" tone="amber" />
          <Metric value={inrShort(annual)} label="annual renewal cost" tone="blue" />
        </View>

        {expired ? (
          <Card style={{ borderWidth: 1.5, borderColor: '#E9BDB7' }}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
              <AlertIcon size={20} c={color.negative} />
              <Text style={s.h}>Police verification lapsed 16 days ago</Text>
            </View>
            <Text style={s.body}>
              Nest PG cannot legally take a new resident until this is refiled. It is your item, not
              the owner's, and it blocks three bed allocations.
            </Text>
            <Button label="Refile today" onPress={() => say('Refiling started — forms pre-filled')} style={{ marginTop: space(4) }} />
          </Card>
        ) : null}

        <ChipRow
          items={[{ label: 'All' }, { label: 'Needs action' }, { label: 'Owner' }, { label: 'Society' }, { label: 'Manager' }]}
          value={filter}
          onChange={setFilter}
        />

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {list.map((c, i) => {
            const isOpen = open === c.id;
            return (
              <View key={c.id}>
                <ListRow
                  left={<View style={s.icon}><ShieldIcon size={20} c={c.state === 'Expired' ? color.negative : color.accent} /></View>}
                  title={c.item}
                  subtitle={c.property}
                  meta={c.daysLeft < 0 ? `Expired ${count(Math.abs(c.daysLeft), 'day')} ago` : `Expires ${c.expires} · ${count(c.daysLeft, 'day')}`}
                  right={<StatusPill text={c.state} tone={stateTone[c.state]} />}
                  onPress={() => setOpen(isOpen ? null : c.id)}
                  last={i === list.length - 1}
                  tone={c.state === 'Expired' ? 'red' : c.state === 'Due soon' ? 'amber' : undefined}
                />
                {isOpen ? (
                  <View style={{ paddingBottom: space(3) }}>
                    <KeyValue k="Issued by" v={c.authority} />
                    <KeyValue k="Whose item" v={c.owner} />
                    <KeyValue k="Renewal cost" v={c.costPaise ? inr(c.costPaise) : 'No fee'} last />
                    <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                      <Button label="Start renewal" small onPress={() => say(`${c.item} renewal started`)} style={{ flex: 1 }} />
                      <Button label="Certificate" tone="secondary" small onPress={() => {}} style={{ flex: 1 }} />
                    </View>
                  </View>
                ) : null}
              </View>
            );
          })}
        </Card>

        <Card>
          <Text style={s.h}>Who gets told, and when</Text>
          <KeyValue k="90 days out" v="Owner or committee is notified" />
          <KeyValue k="60 days out" v="Appears on your worklist" tone="amber" />
          <KeyValue k="30 days out" v="Escalates to the agency principal" tone="amber" />
          <KeyValue k="On expiry" v="Blocks lettings and new allocations" tone="red" last />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  icon: { width: 38, height: 38, borderRadius: 19, backgroundColor: '#F3F7FB', alignItems: 'center', justifyContent: 'center' },
  h: { ...font.h3, color: color.inkStrong, flex: 1 },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
});

/** This screen has no endpoint behind it yet — say so rather than let a
 * manager act on figures that are not theirs. */
const DemoNote = ({ issue }: { issue: number }) => (
  <Text style={sDemo.note}>
    Demonstration data — this screen has no API behind it yet (#{issue}).
  </Text>
);

const sDemo = StyleSheet.create({
  note: { ...font.small, color: color.inkFaint, marginHorizontal: space(4), marginTop: space(3) },
});
