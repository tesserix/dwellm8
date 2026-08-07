import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, Card, ChevronLeft, ListRow, KeyValue, StatusPill, Avatar,
  BellIcon, DocIcon, GlobeIcon, ShieldIcon, HomeIcon,
  color, font, inr, space, useBack,
} from '@dwellm8/mobile-shared';
import { seeker } from '../src/data/mock';

/** Your profile — the thing listers see when you apply. */
export default function Profile() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <AppHeader
        title="You"
        showCaret={false}
        left={<Pressable onPress={goBack} hitSlop={10}><ChevronLeft size={28} w={2.4} /></Pressable>}
        right={<Pressable hitSlop={10}><Text style={s.logout}>Log out</Text></Pressable>}
      />
      <ScrollView contentContainerStyle={{ paddingBottom: space(10) }}>
        <View style={s.hero}>
          <Avatar initials={seeker.initials} size={96} tone="blue" />
          <Text style={s.name}>{seeker.name}</Text>
          <StatusPill text="Verified seeker" tone="green" dot />
          <Text style={s.detail}>{seeker.phone}</Text>
        </View>

        <Card>
          <Text style={s.h}>What listers can see</Text>
          <KeyValue k="Name and photo" v="Yes" />
          <KeyValue k="Employment type" v="Yes, if you share it" />
          <KeyValue k="Inspections attended" v="Yes — it helps you" tone="green" />
          <KeyValue k="Your phone number" v="Only after you apply" />
          <KeyValue k="Salary or ID number" v="Never" tone="red" last />
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow left={<HomeIcon size={20} />} title="Searching in" subtitle={`${seeker.city} · up to ${inr(seeker.budgetPaise, { noPaise: true })}`} onPress={() => router.push('/(tabs)')} />
          <ListRow left={<BellIcon size={20} />} title="Alerts" subtitle="New matches only, never a digest" onPress={() => {}} />
          <ListRow left={<DocIcon size={20} />} title="Your documents" subtitle="Shared one at a time, with your consent" onPress={() => {}} />
          <ListRow left={<ShieldIcon size={20} />} title="Report a listing" subtitle="Wrong rent, already let, or a fee demanded" onPress={() => {}} />
          <ListRow left={<GlobeIcon size={20} />} title="Language" subtitle="English · हिन्दी · ಕನ್ನಡ" onPress={() => {}} last />
        </Card>

        <Text style={s.version}>Dwellm8 Find {seeker.version} · demonstration data</Text>
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
  version: { ...font.small, color: color.inkFaint, textAlign: 'center', marginTop: space(5) },
});
