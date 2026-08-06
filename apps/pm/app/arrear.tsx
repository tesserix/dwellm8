import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, Timeline, Toast,
  ListRow, Avatar, ActionBar, PhoneIcon, ChatIcon, RupeeIcon, RefreshIcon, Field,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { arrears, collectionActions } from '../src/data/mock';
import { historyByLease } from '../src/data/automations';

/**
 * One tenancy in arrears — the whole recovery decision on a single screen.
 *
 * Every action here is recorded against the tenancy: a manager standing at a
 * door can log what happened without opening a laptop, and nothing about the
 * ledger changes silently.
 */

export default function ArrearScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const a = arrears.find((x) => x.id === id) ?? arrears[0];

  const [done, setDone] = useState<string | null>(null);
  const [note, setNote] = useState('');

  const act = (label: string) => {
    setDone(label);
    setTimeout(() => setDone(null), 2600);
  };

  return (
    <>
      <BackHeader title={a.tenant} subtitle={a.unit} onBack={() => router.back()} />
      <Screen>
        {done ? <Toast text={`${done} — recorded against the tenancy`} /> : null}

        <Card>
          <View style={s.head}>
            <Avatar initials={a.initials} size={48} tone={a.daysLate > 20 ? 'red' : 'blue'} />
            <View style={{ flex: 1 }}>
              <Text style={s.due}>{inr(a.duePaise)}</Text>
              <Text style={s.dueSub}>overdue since {a.dueSince} · {a.daysLate} days</Text>
            </View>
            <StatusPill text={a.stage} tone={a.stage === 'Notice' || a.stage === 'Escalated' ? 'red' : 'amber'} />
          </View>

          <View style={{ marginTop: space(2) }}>
            <KeyValue k="Autopay mandate" v={a.mandate} tone={a.mandate === 'Active' ? 'green' : 'red'} />
            <KeyValue k="Last contact" v={a.lastContact} />
            <KeyValue k="Promise to pay" v={a.promiseToPay ?? 'None recorded'} tone={a.promiseToPay ? 'amber' : undefined} />
            <KeyValue k="Phone" v={a.phone} last />
          </View>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Button label="Call" tone="secondary" small icon={<PhoneIcon size={17} c={color.accent} />} onPress={() => act('Call logged')} style={{ flex: 1 }} />
            <Button label="WhatsApp" tone="secondary" small icon={<ChatIcon size={17} c={color.accent} />} onPress={() => act('Reminder sent')} style={{ flex: 1 }} />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>What the ledger shows</Text>
          <KeyValue k="Rent — July 2026" v={inr(a.duePaise)} />
          <KeyValue k="Late fee (not yet applied)" v="₹0.00" />
          <KeyValue k="Deposit held" v={inr(a.duePaise * 2)} />
          <KeyValue k="Balance outstanding" v={inr(a.duePaise)} tone="red" last />
          <Text style={s.note}>
            Deposit is never set against rent without the tenant's written consent — it appears here as
            context only.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>Actions</Text>
          {collectionActions.map((c, i) => (
            <ListRow
              key={c.id}
              title={c.label}
              subtitle={c.hint}
              onPress={() => (c.id === 'a3' ? router.push(`/receipt?id=${a.id}`) : act(c.label))}
              last={i === collectionActions.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>Add a note</Text>
          <Field
            label="Visible to your team and the audit trail"
            value={note}
            onChange={setNote}
            placeholder="Tenant says salary is delayed to 31 July…"
            multiline
          />
          <Button label="Save note" tone="secondary" onPress={() => { setNote(''); act('Note saved'); }} />
        </Card>

        <Card>
          <Text style={s.h}>History</Text>
          <Timeline
            items={[
              { at: '05 Jul', what: 'Rent invoice raised' },
              { at: '06 Jul', what: 'Autopay presented and failed — insufficient funds' },
              { at: '08 Jul', what: 'Automatic WhatsApp reminder delivered' },
              { at: '15 Jul', what: 'Second reminder delivered' },
              { at: '26 Jul', what: 'You called — tenant asked for time' },
              { at: 'Now', what: 'Escalation to notice available', done: false },
            ]}
          />
        </Card>
        <Card>
          <View style={sAuto.head}>
            <RefreshIcon size={19} c={color.inkSoft} />
            <Text style={sAuto.h}>What ran by itself</Text>
          </View>
          <Text style={sAuto.body}>
            Every line here was an automation rather than a person. ADR-0033: a record has to be
            able to say which one, or "a reminder was sent" is all anybody ever learns.
          </Text>
          {(historyByLease['lease-a302'] ?? []).map((line, i, all) => (
            <View
              key={line.id}
              style={[sAuto.line, i === all.length - 1 && { borderBottomWidth: 0 }]}
            >
              <View style={{ flex: 1 }}>
                <Text style={sAuto.action}>{line.action}</Text>
                <Text style={sAuto.meta}>
                  {line.automationName} · {line.occurredAt}
                </Text>
                {line.detail ? <Text style={sAuto.meta}>{line.detail}</Text> : null}
              </View>
              <StatusPill
                text={
                  line.outcome === 'acted' ? 'Done'
                  : line.outcome === 'awaiting_approval' ? 'Needs you'
                  : line.outcome === 'failed' ? 'Failed'
                  : 'Skipped'
                }
                tone={
                  line.outcome === 'acted' ? 'green'
                  : line.outcome === 'awaiting_approval' ? 'amber'
                  : line.outcome === 'failed' ? 'red'
                  : 'neutral'
                }
              />
            </View>
          ))}
          <Button
            label="All automations"
            tone="secondary"
            small
            style={{ marginTop: space(4) }}
            onPress={() => router.push('/automations')}
          />
        </Card>

      </Screen>

      <ActionBar>
        <Button label="Record payment" icon={<RupeeIcon size={18} c="#FFF" />} onPress={() => router.push(`/receipt?id=${a.id}`)} style={{ flex: 1 }} />
        <Button label="Escalate" tone="secondary" onPress={() => act('Escalated to notice')} style={{ flex: 1 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  head: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  due: { ...font.h1, color: color.negative },
  dueSub: { ...font.small, color: color.inkSoft, marginTop: 2 },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});

const sAuto = StyleSheet.create({
  head: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: 8, lineHeight: 21 },
  line: {
    flexDirection: 'row', alignItems: 'flex-start', gap: 10,
    paddingVertical: space(3), borderBottomWidth: 1, borderBottomColor: color.line,
  },
  action: { ...font.label, color: color.inkStrong },
  meta: { ...font.small, color: color.inkSoft, marginTop: 2 },
});
