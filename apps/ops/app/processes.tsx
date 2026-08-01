import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Segmented, StatusPill, Button, Metric, ProgressBar,
  Toast, AlertIcon, ClockIcon, PlusIcon,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { checklists, outstanding, processLabel, settled, startable } from '../src/data/checklists';

/**
 * Every multi-step process under way, across the portfolio. dwellm8#202.
 *
 * The ordering is the argument: late first, because a process nobody is working
 * looks exactly like one under way on every other screen, and the whole point of
 * this one is that it does not.
 */

const processTone: Record<string, Tone> = {
  move_in: 'green',
  move_out: 'amber',
  owner_onboarding: 'violet',
  manager_handover: 'blue',
  tenancy_renewal: 'blue',
};

export default function Processes() {
  const router = useRouter();
  const [view, setView] = useState('Open');
  const [toast, setToast] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const list = checklists
    .filter((c) => (view === 'Open' ? c.state === 'open' : view === 'Late' ? c.state === 'open' && c.daysOverdue > 0 : c.state !== 'open'))
    .sort((a, b) => b.daysOverdue - a.daysOverdue);

  const open = checklists.filter((c) => c.state === 'open');
  const late = open.filter((c) => c.daysOverdue > 0);
  const blocking = open.reduce((n, c) => n + outstanding(c).length, 0);

  return (
    <>
      <BackHeader title="Processes" subtitle="Move-ins, move-outs, onboarding" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={String(open.length)} label="under way" tone="blue" />
          <Metric value={String(late.length)} label="running late" tone={late.length ? 'amber' : 'neutral'} />
          <Metric value={String(blocking)} label="blocking steps" tone="violet" />
        </View>

        <View style={{ marginTop: space(3) }}>
          <Segmented items={['Open', 'Late', 'Closed']} value={view} onChange={setView} />
        </View>

        {list.map((c) => {
          const blocked = outstanding(c);
          const done = settled(c);
          return (
            <Pressable key={c.id} onPress={() => router.push(`/checklist?id=${c.id}`)}>
              <Card>
                <View style={s.top}>
                  <StatusPill text={processLabel[c.process]} tone={processTone[c.process]} />
                  {c.state !== 'open' ? (
                    <StatusPill text={c.state === 'completed' ? 'Finished' : 'Abandoned'} tone={c.state === 'completed' ? 'green' : 'neutral'} />
                  ) : c.daysOverdue > 0 ? (
                    <StatusPill text={`${c.daysOverdue} days late`} tone="red" dot />
                  ) : null}
                </View>

                <Text style={s.unit}>{c.unit}</Text>
                <Text style={s.meta}>{c.locality} · anchored {c.anchorOn}</Text>

                <View style={{ marginTop: space(3) }}>
                  <ProgressBar
                    pct={Math.round((done / c.tasks.length) * 100)}
                    tint={c.daysOverdue > 0 ? color.negative : color.accent}
                  />
                  <Text style={s.progress}>{done} of {c.tasks.length} steps settled</Text>
                </View>

                {blocked.length ? (
                  <View style={s.blockRow}>
                    <AlertIcon size={16} c="#B0731C" />
                    <Text style={s.blockText}>
                      {blocked.length} blocking {blocked.length === 1 ? 'step' : 'steps'} — next is {blocked[0].title.toLowerCase()}
                    </Text>
                  </View>
                ) : null}
              </Card>
            </Pressable>
          );
        })}

        {!list.length ? (
          <Card>
            <Text style={s.empty}>Nothing in this view.</Text>
          </Card>
        ) : null}

        {view !== 'Closed' ? (
          <Card>
            <View style={s.startHead}>
              <ClockIcon size={20} c={color.inkSoft} />
              <Text style={s.h}>Start a process</Text>
            </View>
            <Text style={s.help}>
              One action creates every step with its owner and its due date. The dates come from the
              anchor — the move-out date, the handover date — so a checklist started late is still
              dated from when the work actually falls.
            </Text>

            {starting ? (
              startable.map((o, i) => (
                <Pressable
                  key={o.unit}
                  onPress={() => { setStarting(false); say(`${processLabel[o.process]} started for ${o.unit}`); }}
                  style={[s.option, i === startable.length - 1 && { borderBottomWidth: 0 }]}
                >
                  <View style={{ flex: 1 }}>
                    <Text style={s.optionTitle}>{processLabel[o.process]} · {o.unit}</Text>
                    <Text style={s.optionHint}>{o.hint}</Text>
                  </View>
                  <PlusIcon size={18} c={color.accent} />
                </Pressable>
              ))
            ) : (
              <Button
                label="Start a process"
                onPress={() => setStarting(true)}
                icon={<PlusIcon size={17} c="#FFFFFF" />}
                style={{ marginTop: space(4) }}
                small
              />
            )}
          </Card>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(3) },
  top: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
  unit: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  meta: { ...font.small, color: color.inkSoft, marginTop: 4 },
  progress: { ...font.small, color: color.inkSoft, marginTop: 8 },
  blockRow: { flexDirection: 'row', alignItems: 'center', gap: 7, marginTop: space(3) },
  blockText: { ...font.small, color: '#B0731C', flex: 1, fontWeight: '600' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(5) },
  startHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  h: { ...font.h3, color: color.inkStrong },
  help: { ...font.body, color: color.inkSoft, marginTop: 8, lineHeight: 21 },
  option: {
    flexDirection: 'row', alignItems: 'center', gap: 10,
    paddingVertical: space(3), borderBottomWidth: 1, borderBottomColor: color.line,
  },
  optionTitle: { ...font.label, color: color.inkStrong },
  optionHint: { ...font.small, color: color.inkSoft, marginTop: 2 },
});
