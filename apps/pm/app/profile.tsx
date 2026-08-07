import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, Card, ChevronLeft, ListRow, KeyValue, StatusPill, Avatar, Button,
  BellIcon, DocIcon, GlobeIcon, RefreshIcon, ShieldIcon, UsersIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { useAccount } from '../src/data/account';
import { useSession } from '../src/auth/session';
import { version } from '../package.json';

const constitutionLabel: Record<string, string> = {
  individual: 'Individual', proprietorship: 'Sole proprietorship', partnership: 'Partnership firm',
  llp: 'LLP', company: 'Company', trust: 'Trust or society',
};

/** Who you are, what you may do, and what the app is holding for you. */
export default function Profile() {
  const router = useRouter();
  const me = useAccount();
  const { signOut } = useSession();

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <AppHeader
        title="You"
        showCaret={false}
        left={
          <Pressable accessibilityRole="button" accessibilityLabel="Back"
            onPress={() => router.back()} hitSlop={10}>
            <ChevronLeft size={28} w={2.4} />
          </Pressable>
        }
        right={
          <Pressable onPress={() => void signOut()} hitSlop={10}>
            <Text style={s.logout}>Log out</Text>
          </Pressable>
        }
      />
      <ScrollView contentContainerStyle={{ paddingBottom: space(10) }}>
        <View style={s.hero}>
          <Avatar initials={me.initials || '—'} size={96} tone="blue" />
          <Text style={s.name}>{me.name || (me.loading ? '…' : 'Not signed in')}</Text>
          {me.firmName ? <StatusPill text={me.firmName} tone="blue" /> : null}
          {me.email ? <Text style={s.detail}>{me.email}</Text> : null}
          {me.phone ? <Text style={s.detail}>{me.phone}</Text> : null}
        </View>

        <Card>
          <Text style={s.h}>What you may do</Text>
          <KeyValue k="Acting for" v={me.firmName || 'No firm registered yet'} />
          {me.constitution ? (
            <KeyValue k="Constituted as" v={constitutionLabel[me.constitution] ?? me.constitution} />
          ) : null}
          <KeyValue
            k="May hold a mandate"
            v={me.mayManage ? 'Yes' : 'Not yet — the registration is incomplete'}
            tone={me.mayManage ? 'green' : 'red'}
          />
          <KeyValue k="Properties in scope" v={String(me.properties)} last />
          <Text style={s.note}>
            Permissions are scoped to this organisation. Switching to another organisation switches
            the whole context — nothing is ever merged across the two.
          </Text>
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow left={<UsersIcon size={20} />} title="Switch portfolio" subtitle={me.firmName || 'Choose whose properties you are acting on'} onPress={() => router.push('/switcher')} />
          <ListRow left={<BellIcon size={20} />} title="Notifications" subtitle="SLA breaches, failed mandates, owner approvals" onPress={() => {}} />
          <ListRow left={<RefreshIcon size={20} />} title="Offline queue" subtitle="2 items waiting to sync" onPress={() => {}} />
          <ListRow left={<GlobeIcon size={20} />} title="Language" subtitle="English · हिन्दी available" onPress={() => {}} />
          <ListRow left={<ShieldIcon size={20} />} title="Privacy and data" subtitle="What Dwellm8 stores about you" onPress={() => {}} />
          <ListRow left={<DocIcon size={20} />} title="Terms and conditions" onPress={() => {}} last />
        </Card>

        <Button label="Web console" tone="secondary" onPress={() => {}} style={{ marginHorizontal: space(4) }} />
        <Text style={s.version}>Dwellm8 PM {version}</Text>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  logout: { ...font.title, color: color.accent },
  hero: { alignItems: 'center', paddingVertical: space(7), gap: 6 },
  name: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  detail: { ...font.body, color: color.ink },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  version: { ...font.small, color: color.inkFaint, textAlign: 'center', marginTop: space(5) },
});
