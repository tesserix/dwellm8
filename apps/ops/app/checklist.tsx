import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, StatusPill, Button, Toast, KeyValue, ProgressBar, Field,
  AlertIcon, CheckCircleIcon, ClockIcon, ShieldIcon,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import {
  checklists, outstanding, processLabel, settled, waitingOn,
  type Checklist, type ChecklistTask,
} from '../src/data/checklists';

/**
 * One process, step by step. dwellm8#202, ADR-0032.
 *
 * Two things this screen is careful about, because the API is careful about them
 * and a screen that let somebody try anyway would turn a good refusal into a
 * failed request:
 *
 *  - a step waiting on another says what it waits on, before the tap;
 *  - a blocking step cannot be skipped at all, and the reason is on the row.
 *
 * The tenancy banner is the acceptance criterion made visible: while blocking
 * steps are outstanding the move-out names them and says the tenancy will not
 * close, rather than letting somebody find out at the close.
 */

const stateTone: Record<ChecklistTask['state'], Tone> = {
  done: 'green', skipped: 'neutral', pending: 'blue', blocked: 'neutral',
};

const stateLabel: Record<ChecklistTask['state'], string> = {
  done: 'Done', skipped: 'Skipped', pending: 'To do', blocked: 'Waiting',
};

export default function ChecklistScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const source = checklists.find((c) => c.id === id) ?? checklists[0];

  const [c, setC] = useState<Checklist>(source);
  const [toast, setToast] = useState<string | null>(null);
  const [skipping, setSkipping] = useState<string | null>(null);
  const [reason, setReason] = useState('');

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 3200);
  };

  // Settling a step releases whatever was waiting only on it — the same rule the
  // schema's trigger applies, so the screen never shows a state the API would not.
  const settle = (step: ChecklistTask, to: 'done' | 'skipped') => {
    const tasks = c.tasks.map((t) => (t.stepCode === step.stepCode ? { ...t, state: to } : t));
    const isSettled = (code: string) => {
      const t = tasks.find((x) => x.stepCode === code);
      return t?.state === 'done' || t?.state === 'skipped';
    };
    setC({
      ...c,
      tasks: tasks.map((t) =>
        t.state === 'blocked' && t.dependsOn.every(isSettled) ? { ...t, state: 'pending' } : t,
      ),
    });
    say(to === 'done' ? `${step.title} marked done` : `${step.title} skipped`);
  };

  const blocked = outstanding(c);
  const done = settled(c);
  const gatesTheTenancy = c.process === 'move_out';

  return (
    <>
      <BackHeader
        title={processLabel[c.process]}
        subtitle={`${c.unit} · anchored ${c.anchorOn}`}
        onBack={() => router.back()}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={s.top}>
            <StatusPill
              text={c.state === 'open' ? 'Under way' : c.state === 'completed' ? 'Finished' : 'Abandoned'}
              tone={c.state === 'completed' ? 'green' : c.state === 'open' ? 'blue' : 'neutral'}
            />
            {c.daysOverdue > 0 && c.state === 'open' ? (
              <StatusPill text={`${c.daysOverdue} days late`} tone="red" dot />
            ) : null}
          </View>

          <View style={{ marginTop: space(4) }}>
            <ProgressBar
              pct={Math.round((done / c.tasks.length) * 100)}
              tint={blocked.length ? color.accent : color.positive}
            />
            <Text style={s.progress}>{done} of {c.tasks.length} steps settled</Text>
          </View>

          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Template" v={c.templateName} />
            <KeyValue k="Anchor date" v={c.anchorOn} />
            <KeyValue k="Blocking outstanding" v={String(blocked.length)} tone={blocked.length ? 'amber' : 'green'} last />
          </View>
        </Card>

        {gatesTheTenancy && c.state === 'open' ? (
          <Card>
            <View style={s.gateHead}>
              {blocked.length ? <ShieldIcon size={19} c="#B0731C" /> : <CheckCircleIcon size={19} c={color.positive} />}
              <Text style={s.h}>{blocked.length ? 'The tenancy cannot be closed yet' : 'The tenancy can be closed'}</Text>
            </View>
            {blocked.length ? (
              <>
                <Text style={s.gateBody}>
                  These steps have to be settled first. Ending the tenancy before them would settle the
                  deposit against an inspection nobody did.
                </Text>
                {blocked.map((t) => (
                  <View key={t.stepCode} style={s.gateItem}>
                    <View style={s.dot} />
                    <Text style={s.gateItemText}>{t.title} · {t.assignee ?? t.ownerRole.replace('_', ' ')}</Text>
                  </View>
                ))}
              </>
            ) : (
              <Text style={s.gateBody}>
                Every blocking step is settled. Finishing the move-out releases the close.
              </Text>
            )}
          </Card>
        ) : null}

        {c.tasks.map((t) => {
          const waits = waitingOn(c, t);
          const isSettledStep = t.state === 'done' || t.state === 'skipped';
          return (
            <Card key={t.stepCode}>
              <View style={s.top}>
                <StatusPill text={stateLabel[t.state]} tone={stateTone[t.state]} />
                {t.blocking ? <StatusPill text="Blocking" tone="amber" /> : null}
                <View style={{ flex: 1 }} />
                <Text style={s.due}>{t.dueOn}</Text>
              </View>

              <Text style={[s.title, isSettledStep && s.titleSettled]}>{t.title}</Text>
              <Text style={s.owner}>{t.assignee ?? t.ownerRole.replace('_', ' ')}</Text>

              {waits.length ? (
                <View style={s.waitRow}>
                  <ClockIcon size={15} c={color.inkSoft} />
                  <Text style={s.wait}>Waits on {waits.join(', ').toLowerCase()}</Text>
                </View>
              ) : null}

              {c.state === 'open' && !isSettledStep ? (
                skipping === t.stepCode ? (
                  <View style={{ marginTop: space(3) }}>
                    <Field
                      label="Why does this step not apply?"
                      value={reason}
                      onChange={setReason}
                      placeholder="Recorded against the process"
                    />
                    <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                      <Button
                        label="Skip this step"
                        small
                        style={{ flex: 1 }}
                        onPress={() => {
                          if (!reason.trim()) {
                            say('A skipped step must say why');
                            return;
                          }
                          settle(t, 'skipped');
                          setSkipping(null);
                          setReason('');
                        }}
                      />
                      <Button label="Cancel" tone="secondary" small style={{ flex: 1 }} onPress={() => setSkipping(null)} />
                    </View>
                  </View>
                ) : (
                  <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
                    <Button
                      label="Mark done"
                      small
                      style={{ flex: 1 }}
                      disabled={waits.length > 0}
                      onPress={() => settle(t, 'done')}
                    />
                    {t.blocking ? (
                      // Not a disabled button: a control that cannot ever be used is
                      // a question, and this is the answer to it.
                      <View style={s.cannotSkip}>
                        <AlertIcon size={14} c={color.inkSoft} />
                        <Text style={s.cannotSkipText}>Blocking steps cannot be skipped</Text>
                      </View>
                    ) : (
                      <Button
                        label="Does not apply"
                        tone="secondary"
                        small
                        style={{ flex: 1 }}
                        onPress={() => { setSkipping(t.stepCode); setReason(''); }}
                      />
                    )}
                  </View>
                )
              ) : null}
            </Card>
          );
        })}

        {c.state === 'open' ? (
          <Card>
            <Text style={s.h}>Finish this process</Text>
            <Text style={s.gateBody}>
              {blocked.length
                ? `Refused while ${blocked.length} blocking ${blocked.length === 1 ? 'step is' : 'steps are'} outstanding.`
                : 'Every blocking step is settled, so this can be closed.'}
            </Text>
            <Button
              label="Finish"
              small
              tone={blocked.length ? 'secondary' : 'primary'}
              style={{ marginTop: space(4) }}
              onPress={() =>
                blocked.length
                  ? say(`Not finished — ${blocked.map((t) => t.title).join(', ')}`)
                  : setC({ ...c, state: 'completed' })
              }
            />
          </Card>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  top: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  progress: { ...font.small, color: color.inkSoft, marginTop: 8 },
  h: { ...font.h3, color: color.inkStrong },
  gateHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  gateBody: { ...font.body, color: color.inkSoft, marginTop: 8, lineHeight: 21 },
  gateItem: { flexDirection: 'row', alignItems: 'center', gap: 9, marginTop: 8 },
  dot: { width: 6, height: 6, borderRadius: 3, backgroundColor: '#B0731C' },
  gateItemText: { ...font.small, color: color.inkStrong, flex: 1 },
  due: { ...font.small, color: color.inkSoft },
  title: { ...font.label, color: color.inkStrong, marginTop: space(3), lineHeight: 21 },
  titleSettled: { color: color.inkSoft },
  owner: { ...font.small, color: color.inkSoft, marginTop: 3, textTransform: 'capitalize' },
  waitRow: { flexDirection: 'row', alignItems: 'center', gap: 7, marginTop: space(3) },
  wait: { ...font.small, color: color.inkSoft, flex: 1 },
  cannotSkip: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 4 },
  cannotSkipText: { ...font.small, color: color.inkSoft, flex: 1 },
});
