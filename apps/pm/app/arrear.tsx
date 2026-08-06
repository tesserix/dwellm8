import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator, Linking } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, Toast,
  Avatar, ActionBar, PhoneIcon, ChatIcon, RupeeIcon, Field,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { useArrear } from '../src/data/arrear';
import { useOpsThread } from '../src/data/worklists';

/**
 * One tenancy in arrears — the whole recovery decision on a single screen.
 *
 * Only what the ledger and the message thread actually say appears here. A
 * deposit or a promise to pay that nobody recorded is worse than a blank: a
 * manager standing at a door will act on it.
 */

export default function ArrearScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, row, owes } = useArrear(id);
  const thread = useOpsThread(id);

  const [note, setNote] = useState('');
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  const say = (text: string) => {
    setSent(text);
    setTimeout(() => setSent(null), 2600);
  };

  const send = async () => {
    setSending(true);
    setFailure(null);
    try {
      await thread.send(note.trim());
      setNote('');
      say('Sent to the tenant');
    } catch (err) {
      setFailure((err as Error).message);
    } finally {
      setSending(false);
    }
  };

  if (loading) {
    return (
      <>
        <BackHeader title="Tenancy" onBack={() => router.back()} />
        <Screen><View style={s.wait}><ActivityIndicator /></View></Screen>
      </>
    );
  }

  if (!row) {
    return (
      <>
        <BackHeader title="Tenancy" onBack={() => router.back()} />
        <Screen><Card><Text style={s.note}>{error ?? 'That tenancy is not on this roster.'}</Text></Card></Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title={row.unit} subtitle={row.property} onBack={() => router.back()} />
      <Screen>
        {sent ? <Toast text={sent} /> : null}

        <Card>
          <View style={s.head}>
            <Avatar initials={row.unit.slice(0, 2).toUpperCase()} size={48} tone={owes ? 'red' : 'green'} />
            <View style={{ flex: 1 }}>
              <Text style={[s.due, !owes && { color: color.positive }]}>
                {inr(Math.max(0, row.due_amount_minor))}
              </Text>
              <Text style={s.dueSub}>outstanding as of {row.as_of}</Text>
            </View>
            <StatusPill text={owes ? 'Owes' : 'Clear'} tone={owes ? 'red' : 'green'} />
          </View>

          <View style={{ marginTop: space(2) }}>
            <KeyValue k="Rent" v={inr(row.rent_amount_minor)} />
            <KeyValue k="Locality" v={row.locality} />
            <KeyValue k="Phone" v={row.phone || 'Not on file'} last />
          </View>

          {row.phone ? (
            <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
              <Button label="Call" tone="secondary" small icon={<PhoneIcon size={17} c={color.accent} />}
                onPress={() => Linking.openURL(`tel:${row.phone}`)} style={{ flex: 1 }} />
              <Button label="Message" tone="secondary" small icon={<ChatIcon size={17} c={color.accent} />}
                onPress={() => router.push(`/thread?id=${row.lease_id}`)} style={{ flex: 1 }} />
            </View>
          ) : null}
        </Card>

        <Card>
          <Text style={s.h}>Send a message</Text>
          <Text style={s.note}>
            It reaches the tenant in their own app and stays on the tenancy’s thread, so whoever
            picks this up next can see what was already said.
          </Text>
          <Field
            label="To the tenant"
            value={note}
            onChange={setNote}
            placeholder="The August rent is outstanding. Can you let me know when it will be paid?"
            multiline
          />
          <Button label={sending ? 'Sending…' : 'Send'} tone="secondary"
            onPress={send} disabled={sending || !note.trim()} />
          {failure ? <Text style={s.error} accessibilityRole="alert">{failure}</Text> : null}
        </Card>

        <Card>
          <Text style={s.h}>What has been said</Text>
          {thread.loading ? <View style={s.wait}><ActivityIndicator /></View> : null}
          {thread.data.map((m) => (
            <View key={m.message_id} style={s.line}>
              <View style={{ flex: 1 }}>
                <Text style={s.action}>{m.body}</Text>
                <Text style={s.meta}>{m.sender === 'resident' ? 'Tenant' : 'You'} · {m.sent_at}</Text>
              </View>
            </View>
          ))}
          {!thread.loading && !thread.data.length ? (
            <Text style={s.note}>Nothing yet on this tenancy.</Text>
          ) : null}
        </Card>
      </Screen>

      <ActionBar>
        <Button
          label="Record payment"
          icon={<RupeeIcon size={18} c="#FFF" />}
          onPress={() => router.push({
            pathname: '/receipt',
            params: {
              lease: row.lease_id,
              unit: `${row.unit}, ${row.property}`,
              due: String(Math.max(0, row.due_amount_minor)),
            },
          })}
          style={{ flex: 1 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  wait: { paddingVertical: space(6), alignItems: 'center' },
  head: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  due: { ...font.h1, color: color.negative },
  dueSub: { ...font.small, color: color.inkSoft, marginTop: 2 },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(2), lineHeight: 18 },
  error: { ...font.small, color: color.negative, marginTop: space(2) },
  line: { flexDirection: 'row', gap: 10, paddingVertical: space(3), borderBottomWidth: 1, borderBottomColor: color.line },
  action: { ...font.body, color: color.inkStrong },
  meta: { ...font.small, color: color.inkSoft, marginTop: 2 },
});
