import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Segmented, StatusPill, Button, Metric, SwitchRow, Toast,
  AlertIcon, CheckCircleIcon, ClockIcon, RefreshIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import {
  approvals as seededApprovals, automations as seeded, customisedCount, enabledCount,
  type Automation,
} from '../src/data/automations';

/**
 * What runs by itself, and what it is waiting on. dwellm8#200, ADR-0033.
 *
 * Two things this screen is careful about.
 *
 * A switch says what it does before it is turned off — the purpose is on the row,
 * not behind a tooltip — because an automation nobody can explain is one nobody
 * dares switch off and one nobody trusts switched on.
 *
 * And every switch carries what it has actually done. "Acted 31 times, last at
 * 06:00 today" is a line a manager can act on; a toggle with nothing beside it
 * asks them to take the product's word for it.
 */

export default function Automations() {
  const router = useRouter();
  const [list, setList] = useState<Automation[]>(seeded);
  const [approvals, setApprovals] = useState(seededApprovals);
  const [view, setView] = useState('Running');
  const [open, setOpen] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2800);
  };

  const toggle = (key: string) => {
    setList((prev) =>
      prev.map((a) => {
        if (a.key !== key) return a;
        const enabled = !a.enabled;
        const overridden = enabled === a.enabledByDefault
          ? a.overridden.filter((o) => o !== 'enabled')
          : [...new Set([...a.overridden, 'enabled'])];
        say(enabled ? `${a.name} is running again` : `${a.name} is off for this organisation`);
        return { ...a, enabled, overridden };
      }),
    );
  };

  const step = (key: string, param: string, by: number) => {
    setList((prev) =>
      prev.map((a) => {
        if (a.key !== key) return a;
        return {
          ...a,
          params: a.params.map((p) => {
            if (p.name !== param) return p;
            const value = Math.min(p.max, Math.max(p.min, p.value + by));
            return { ...p, value };
          }),
          overridden: [...new Set([...a.overridden, param])],
        };
      }),
    );
  };

  const shown = list.filter((a) =>
    view === 'Running' ? a.enabled : view === 'Off' ? !a.enabled : a.overridden.length > 0,
  );

  return (
    <>
      <BackHeader
        title="Automations"
        subtitle="What runs without being asked"
        onBack={() => router.back()}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={`${enabledCount(list)}/${list.length}`} label="running" tone="green" />
          <Metric value={String(customisedCount(list))} label="customised" tone="blue" />
          <Metric
            value={String(approvals.length)}
            label="need approval"
            tone={approvals.length ? 'amber' : 'neutral'}
          />
        </View>

        {approvals.length ? (
          <Card>
            <View style={s.head}>
              <AlertIcon size={19} c="#B0731C" />
              <Text style={s.h}>Waiting on you</Text>
            </View>
            <Text style={s.body}>
              These stopped instead of acting, because what they wanted to do is over the ceiling
              set for them. Nothing was done.
            </Text>
            {approvals.map((a) => (
              <View key={a.id} style={s.approval}>
                <Text style={s.approvalAction}>{a.action}</Text>
                <Text style={s.approvalMeta}>
                  {a.automationName} · {a.subject}
                </Text>
                <Text style={s.approvalMeta}>
                  {inr(a.amountMinor)} against a ceiling of {inr(a.ceilingMinor)} · {a.requestedAt}
                </Text>
                <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                  <Button
                    label="Approve"
                    small
                    style={{ flex: 1 }}
                    onPress={() => {
                      setApprovals((p) => p.filter((x) => x.id !== a.id));
                      say('Approved — it will act on the next run');
                    }}
                  />
                  <Button
                    label="Decline"
                    tone="secondary"
                    small
                    style={{ flex: 1 }}
                    onPress={() => {
                      setApprovals((p) => p.filter((x) => x.id !== a.id));
                      say('Declined — it may ask again');
                    }}
                  />
                </View>
              </View>
            ))}
          </Card>
        ) : null}

        <View style={{ marginTop: space(3) }}>
          <Segmented items={['Running', 'Off', 'Customised']} value={view} onChange={setView} />
        </View>

        {shown.map((a) => {
          const isOpen = open === a.key;
          return (
            <Card key={a.key}>
              <SwitchRow
                label={a.name}
                hint={a.purpose}
                value={a.enabled}
                onChange={() => toggle(a.key)}
                last
              />

              <View style={s.pills}>
                <StatusPill
                  text={a.trigger === 'event' ? 'On an event' : 'Daily'}
                  tone={a.trigger === 'event' ? 'violet' : 'blue'}
                />
                {a.overridden.length ? <StatusPill text="Customised" tone="amber" /> : null}
                {a.failed ? <StatusPill text={`${a.failed} failed`} tone="red" dot /> : null}
              </View>

              <View style={s.activity}>
                {a.acted ? (
                  <CheckCircleIcon size={15} c={color.positive} />
                ) : (
                  <ClockIcon size={15} c={color.inkSoft} />
                )}
                <Text style={s.activityText}>
                  {a.acted ? `Acted ${a.acted} times` : 'Has not acted yet'}
                  {a.lastRunAt ? ` · last run ${a.lastRunAt.toLowerCase()}` : ''}
                </Text>
              </View>

              {a.params.length ? (
                <Pressable onPress={() => setOpen(isOpen ? null : a.key)}>
                  <Text style={s.settingsLink}>
                    {isOpen ? 'Hide settings' : `Settings (${a.params.length})`}
                  </Text>
                </Pressable>
              ) : null}

              {isOpen
                ? a.params.map((p, i) => (
                    <View key={p.name} style={[s.param, i === a.params.length - 1 && { borderBottomWidth: 0 }]}>
                      <View style={{ flex: 1 }}>
                        <Text style={s.paramPurpose}>{p.purpose}</Text>
                        <Text style={s.paramMeta}>
                          {p.unit === 'paise' ? inr(p.value) : `${p.value} ${p.unit}`}
                          {p.value !== p.default
                            ? ` · default ${p.unit === 'paise' ? inr(p.default) : `${p.default}`}`
                            : ' · the default'}
                        </Text>
                      </View>
                      <View style={s.stepper}>
                        <Pressable
                          onPress={() => step(a.key, p.name, p.unit === 'paise' ? -5000 : -1)}
                          style={s.stepButton}
                        >
                          <Text style={s.stepText}>−</Text>
                        </Pressable>
                        <Pressable
                          onPress={() => step(a.key, p.name, p.unit === 'paise' ? 5000 : 1)}
                          style={s.stepButton}
                        >
                          <Text style={s.stepText}>+</Text>
                        </Pressable>
                      </View>
                    </View>
                  ))
                : null}
            </Card>
          );
        })}

        {!shown.length ? (
          <Card>
            <Text style={s.empty}>Nothing in this view.</Text>
          </Card>
        ) : null}

        <Card>
          <View style={s.head}>
            <RefreshIcon size={19} c={color.inkSoft} />
            <Text style={s.h}>How these run</Text>
          </View>
          <Text style={s.body}>
            The daily ones run once each morning, one organisation at a time. The event ones run the
            moment the fact arrives — a tenancy going live starts its move-in without waiting for
            the morning. Switching one off here affects this organisation only and takes effect on
            the next run.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(3) },
  head: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: 8, lineHeight: 21 },
  approval: { marginTop: space(4), paddingTop: space(3), borderTopWidth: 1, borderTopColor: color.line },
  approvalAction: { ...font.label, color: color.inkStrong },
  approvalMeta: { ...font.small, color: color.inkSoft, marginTop: 3 },
  pills: { flexDirection: 'row', gap: 8, marginTop: space(3), flexWrap: 'wrap' },
  activity: { flexDirection: 'row', alignItems: 'center', gap: 7, marginTop: space(3) },
  activityText: { ...font.small, color: color.inkSoft, flex: 1 },
  settingsLink: { ...font.small, color: color.accent, fontWeight: '600', marginTop: space(3) },
  param: {
    flexDirection: 'row', alignItems: 'center', gap: 10,
    paddingVertical: space(3), borderBottomWidth: 1, borderBottomColor: color.line,
  },
  paramPurpose: { ...font.label, color: color.inkStrong },
  paramMeta: { ...font.small, color: color.inkSoft, marginTop: 2 },
  stepper: { flexDirection: 'row', gap: 8 },
  stepButton: {
    width: 34, height: 34, borderRadius: 10, borderWidth: 1, borderColor: color.line,
    alignItems: 'center', justifyContent: 'center',
  },
  stepText: { ...font.h3, color: color.inkStrong, lineHeight: 22 },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(5) },
});
